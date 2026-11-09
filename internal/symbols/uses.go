package symbols

import "github.com/Brg92/agent-code-review/internal/lexer"

// Position is a place in a source; Column counts runes.
type Position struct {
	Line, Column int
}

// Uses returns, in source order, the positions of the identifiers named
// name in a source, the expressions of string templates included and
// comments and string text excluded. It finds names, not meanings: a
// declaration or another symbol with the same name is listed too.
func Uses(table *lexer.Table, src, name string) []Position {
	var out []Position
	var walk func([]lexer.Token)
	walk = func(toks []lexer.Token) {
		for _, t := range toks {
			switch t.Kind {
			case lexer.LineComment, lexer.BlockComment, lexer.DocComment:
				continue
			case lexer.Identifier:
				if t.Text == name {
					out = append(out, Position{Line: t.Line, Column: t.Column})
				}
			}
			walk(t.Inner)
		}
	}
	walk(lexer.Tokenize(table, src))
	return out
}
