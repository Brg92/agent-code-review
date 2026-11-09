package symbols

import (
	"strings"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

// Param is a parameter of a function.
type Param struct {
	Name string
	// Default reports whether the parameter has a default value.
	Default bool
}

// Decl is a declaration of a source file.
type Decl struct {
	Package string
	// Containers are the types the declaration is nested in, outermost
	// first; a type without a name is listed by its declaration word.
	Containers []string
	Name       string
	Kind       Kind
	// Params are the parameters of a function, in order; nil for the
	// other kinds.
	Params []Param
	// Modifiers are the words before the declaration word, or before the
	// type of a member declared without one.
	Modifiers []string
	// Public reports whether other modules can use the declaration: no
	// hiding modifier on it or on the types it is nested in, and a public
	// modifier where the language needs one.
	Public bool
	// Line and Column are those of the name.
	Line, Column int
	// StartLine and EndLine bound the declaration from its first
	// annotation or modifier to the end of its signature.
	StartLine, EndLine int
}

// Identity is what tells a symbol apart from the others: overloads of a
// function share it.
type Identity struct {
	Package, Container, Name string
	Kind                     Kind
}

// ID returns the identity of the declaration.
func (d Decl) ID() Identity {
	return Identity{Package: d.Package, Container: strings.Join(d.Containers, "."), Name: d.Name, Kind: d.Kind}
}

// String writes the identity as a dotted name.
func (id Identity) String() string {
	var parts []string
	for _, p := range []string{id.Package, id.Container, id.Name} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ".")
}

// scope is an open body.
type scope struct {
	// typeBody is set for the body of a type, whose members are
	// declarations; other bodies hold code.
	typeBody bool
	// label is the container name of the type.
	label string
	// public reports whether the type is public; publicInside, whether
	// its members are public without a public modifier.
	public, publicInside bool
}

type extractor struct {
	s    *Spec
	toks []lexer.Token
	// opens and closes are the bracket characters that nest inside a
	// parameter list.
	opens, closes map[string]bool
}

// Extract returns the named declarations of a source at top level and in
// type bodies, in source order; declarations inside function bodies and
// other code are not symbols. Malformed code yields what can be read.
func Extract(spec *Spec, table *lexer.Table, src string) []Decl {
	x := &extractor{s: spec, opens: map[string]bool{}, closes: map[string]bool{}}
	for _, t := range lexer.Tokenize(table, src) {
		switch t.Kind {
		case lexer.LineComment, lexer.BlockComment, lexer.DocComment:
			continue
		}
		x.toks = append(x.toks, t)
	}
	brackets := append([]lexer.Bracket{spec.Body, spec.Parameters, spec.TypeParameters}, table.Brackets...)
	for _, b := range brackets {
		x.opens[b.Open], x.closes[b.Close] = true, true
	}
	return x.run()
}

func (x *extractor) run() []Decl {
	var (
		out     []Decl
		stack   []scope
		pending *scope // a type whose body is not open yet
		header  int    // depth of brackets in the header of the pending type
		handled = -1   // last token of the declaration read last
		pkg     string
	)
	for k, t := range x.toks {
		inScope := (len(stack) == 0 || stack[len(stack)-1].typeBody) && header == 0
		if k > handled && inScope && x.starts(k) {
			if len(stack) == 0 && pkg == "" && x.word(k, x.s.PackageKeyword) {
				pkg, handled = x.dotted(k + 1)
			} else if d, label, end, ok := x.declaration(k, len(stack) > 0); ok {
				handled = end
				parent := scope{public: true}
				if len(stack) > 0 {
					parent = stack[len(stack)-1]
				}
				d.Package = pkg
				for _, s := range stack {
					d.Containers = append(d.Containers, s.label)
				}
				d.Public = parent.public && x.public(d.Modifiers, parent.publicInside)
				if d.Name != "" {
					out = append(out, d)
				}
				pending, header = nil, 0
				if d.Kind == Type {
					pending = &scope{typeBody: true, label: label, public: d.Public, publicInside: x.s.PublicMembersIn[x.declWord(k, end)]}
				}
			}
		}
		if pending != nil {
			header += x.count(t, x.s.Parameters.Open) + x.count(t, x.s.TypeParameters.Open)
			header -= x.count(t, x.s.Parameters.Close) + x.count(t, x.s.TypeParameters.Close)
			if header < 0 {
				header = 0
			}
		}
		switch {
		case x.punct(t, x.s.Body.Open):
			if pending != nil && header == 0 {
				stack = append(stack, *pending)
				pending = nil
			} else {
				stack = append(stack, scope{})
			}
		case x.punct(t, x.s.Body.Close) && len(stack) > 0:
			stack = stack[:len(stack)-1]
		}
	}
	return out
}

// starts reports whether a declaration may start at toks[k]: the first
// token of a line, or one right after a body bracket or an end of a typed
// member.
func (x *extractor) starts(k int) bool {
	if k == 0 || x.toks[k-1].Line != x.toks[k].Line {
		return true
	}
	p := x.toks[k-1]
	if x.punct(p, x.s.Body.Open) || x.punct(p, x.s.Body.Close) {
		return true
	}
	for _, e := range x.s.TypedMemberEnds {
		if x.punct(p, e) {
			return true
		}
	}
	return false
}

// declaration reads a declaration at toks[k]: annotations, modifiers and
// a declaration word, or, in a type body, a typed member. It returns the
// declaration, its container label, its last token and whether there is
// one.
func (x *extractor) declaration(k int, inType bool) (Decl, string, int, bool) {
	i := x.annotations(k)
	var mods []string
	for x.isWord(i) {
		w := x.toks[i].Text
		if kind, ok := x.s.Declarations[w]; ok {
			if x.isWord(i+1) && x.isDeclWord(i+1) {
				// A declaration word used as a modifier, as in fun interface.
				mods = append(mods, w)
				i++
				continue
			}
			return x.named(k, i, kind, mods)
		}
		mods = append(mods, w)
		i = x.annotations(i + 1)
	}
	if inType && x.s.TypedMemberEnds != nil {
		return x.typed(k)
	}
	return Decl{}, "", 0, false
}

// named reads the declaration whose declaration word is toks[i]. The name
// follows the word, past type parameters; for an extension it is the word
// after the last qualifier of the receiver type.
func (x *extractor) named(k, i int, kind Kind, mods []string) (Decl, string, int, bool) {
	word := x.toks[i]
	end := i
	i++
	i = x.skipTypeParameters(i)
	name := -1
	if x.isIdent(i) {
		name, end = i, i
		i++
		for {
			j := x.skipTypeParameters(i)
			if j < len(x.toks) && x.qualifier(x.toks[j]) && x.isIdent(j+1) {
				name, end, i = j+1, j+1, j+2
				continue
			}
			break
		}
	}
	if name < 0 && kind != Type {
		return Decl{}, "", 0, false
	}
	d := Decl{Kind: kind, Modifiers: mods, Line: word.Line, Column: word.Column, StartLine: x.toks[k].Line}
	label := word.Text
	if name >= 0 {
		d.Name, d.Line, d.Column = x.toks[name].Text, x.toks[name].Line, x.toks[name].Column
		label = d.Name
	}
	if kind == Function {
		d.Params = []Param{}
		if j := x.skipTypeParameters(i); j < len(x.toks) && x.punct(x.toks[j], x.s.Parameters.Open) {
			d.Params, end = x.params(j)
		}
	}
	d.EndLine = x.toks[end].Line
	return d, label, end, true
}

// typed reads a member declared as <modifiers> <type> <name>, followed by
// the parameter list of a function or by an end of a property.
func (x *extractor) typed(k int) (Decl, string, int, bool) {
	var words []string
	last := -1 // the last identifier, if it is the last token read
	for i := x.annotations(k); i < len(x.toks); {
		t := x.toks[i]
		switch {
		case t.Kind == lexer.Identifier || t.Kind == lexer.Keyword:
			words = append(words, t.Text)
			last = -1
			if t.Kind == lexer.Identifier {
				last = i
			}
			i++
			continue
		case x.punct(t, x.s.Annotation):
			i, last = x.annotations(i), -1
			continue
		case x.count(t, x.s.TypeParameters.Open) > 0:
			i, last = x.skipTypeParameters(i), -1
			continue
		}
		if last < 0 || last != i-1 {
			if x.punct(t, x.s.Parameters.Open) || x.punct(t, x.s.Body.Open) || x.punct(t, x.s.Body.Close) || x.isEnd(t) {
				return Decl{}, "", 0, false
			}
			i++
			continue
		}
		n := x.toks[last]
		d := Decl{Name: n.Text, Modifiers: words[:len(words)-1], Line: n.Line, Column: n.Column, StartLine: x.toks[k].Line}
		end := last
		switch {
		case x.punct(t, x.s.Parameters.Open):
			d.Kind = Function
			d.Params, end = x.params(i)
		case x.isEnd(t):
			d.Kind = Property
		default:
			return Decl{}, "", 0, false
		}
		d.EndLine = x.toks[end].Line
		return d, d.Name, end, true
	}
	return Decl{}, "", 0, false
}

// params reads the parameter list that opens at toks[open] and returns
// its parameters and the index of its closing bracket, the last token
// when it is not closed.
func (x *extractor) params(open int) ([]Param, int) {
	out := []Param{}
	var seg []lexer.Token
	flush := func() {
		if p, ok := x.param(seg); ok {
			out = append(out, p)
		}
		seg = nil
	}
	depth := 0
	for j := open + 1; j < len(x.toks); j++ {
		t := x.toks[j]
		if depth == 0 && x.punct(t, x.s.Parameters.Close) {
			flush()
			return out, j
		}
		if c := x.closing(t); c > 0 {
			if depth -= c; depth < 0 {
				depth = 0
			}
			continue
		}
		if o := x.opening(t); o > 0 {
			depth += o
			continue
		}
		switch {
		case depth > 0:
		case x.punct(t, x.s.ParameterSeparator):
			flush()
		default:
			seg = append(seg, t)
		}
	}
	flush()
	return out, len(x.toks) - 1
}

// param reads one parameter from its tokens outside brackets: the name is
// the identifier before the parameter type punctuation, or the last
// identifier before the default.
func (x *extractor) param(seg []lexer.Token) (Param, bool) {
	var p Param
	head := seg
	if x.s.ParameterDefault != "" {
		for i, t := range seg {
			if x.punct(t, x.s.ParameterDefault) {
				p.Default, head = true, seg[:i]
				break
			}
		}
	}
	if x.s.ParameterType != "" {
		for i, t := range head {
			if x.punct(t, x.s.ParameterType) && i > 0 && head[i-1].Kind == lexer.Identifier {
				p.Name = head[i-1].Text
				return p, true
			}
		}
	}
	for i := len(head) - 1; i >= 0; i-- {
		if head[i].Kind == lexer.Identifier {
			p.Name = head[i].Text
			return p, true
		}
	}
	return p, false
}

// public reports whether a declaration with these modifiers is public in
// a scope whose members are public by default when publicInside is set.
func (x *extractor) public(mods []string, publicInside bool) bool {
	explicit := false
	for _, m := range mods {
		if x.s.HidingModifiers[m] {
			return false
		}
		explicit = explicit || x.s.PublicModifiers[m]
	}
	return x.s.PublicByDefault || explicit || publicInside
}

// annotations skips the annotations that start at toks[i], with their
// dotted names and arguments, and returns the index after them.
func (x *extractor) annotations(i int) int {
	for i < len(x.toks) && x.punct(x.toks[i], x.s.Annotation) && x.isWord(i+1) {
		i += 2
		for i+1 < len(x.toks) && (x.qualifier(x.toks[i]) || x.punct(x.toks[i], x.s.ParameterType)) && x.isWord(i+1) {
			i += 2
		}
		if i < len(x.toks) && x.punct(x.toks[i], x.s.Parameters.Open) {
			_, close := x.params(i)
			i = close + 1
		}
	}
	return i
}

// skipTypeParameters skips the type parameters that open at toks[i], if
// any, and returns the index after them.
func (x *extractor) skipTypeParameters(i int) int {
	if i >= len(x.toks) || x.count(x.toks[i], x.s.TypeParameters.Open) == 0 {
		return i
	}
	depth := 0
	for ; i < len(x.toks); i++ {
		t := x.toks[i]
		depth += x.count(t, x.s.TypeParameters.Open) - x.count(t, x.s.TypeParameters.Close)
		if depth <= 0 {
			return i + 1
		}
	}
	return i
}

// dotted reads a dotted name that starts at toks[i] and returns it with
// its last token.
func (x *extractor) dotted(i int) (string, int) {
	if !x.isWord(i) {
		return "", i - 1
	}
	name := x.toks[i].Text
	for i+2 < len(x.toks) && x.qualifier(x.toks[i+1]) && x.isWord(i+2) {
		name += "." + x.toks[i+2].Text
		i += 2
	}
	return name, i
}

// declWord returns the declaration word of the declaration in
// toks[k:end+1].
func (x *extractor) declWord(k, end int) string {
	w := ""
	for i := k; i <= end && i < len(x.toks); i++ {
		if x.isDeclWord(i) {
			w = x.toks[i].Text
		}
	}
	return w
}

func (x *extractor) isWord(i int) bool {
	return i < len(x.toks) && (x.toks[i].Kind == lexer.Identifier || x.toks[i].Kind == lexer.Keyword)
}

func (x *extractor) isIdent(i int) bool {
	return i < len(x.toks) && x.toks[i].Kind == lexer.Identifier
}

func (x *extractor) isDeclWord(i int) bool {
	_, ok := x.s.Declarations[x.toks[i].Text]
	return ok
}

func (x *extractor) word(i int, w string) bool {
	return x.isWord(i) && x.toks[i].Text == w
}

func (x *extractor) punct(t lexer.Token, text string) bool {
	return t.Kind == lexer.Punct && t.Text == text
}

func (x *extractor) qualifier(t lexer.Token) bool {
	return t.Kind == lexer.Punct && x.s.Qualifiers[t.Text]
}

func (x *extractor) isEnd(t lexer.Token) bool {
	for _, e := range x.s.TypedMemberEnds {
		if x.punct(t, e) {
			return true
		}
	}
	return false
}

// count returns how many times a punctuation token repeats the one
// character c and nothing else, as >> closes two type parameter lists.
func (x *extractor) count(t lexer.Token, c string) int {
	if t.Kind != lexer.Punct || t.Text == "" || strings.Trim(t.Text, c) != "" {
		return 0
	}
	return len(t.Text) / len(c)
}

// opening and closing count the brackets a token opens or closes.
func (x *extractor) opening(t lexer.Token) int {
	for o := range x.opens {
		if n := x.count(t, o); n > 0 {
			return n
		}
	}
	return 0
}

func (x *extractor) closing(t lexer.Token) int {
	for c := range x.closes {
		if n := x.count(t, c); n > 0 {
			return n
		}
	}
	return 0
}
