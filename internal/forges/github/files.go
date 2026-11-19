package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// files is the core.Files of the provider.
type files struct{ p *Provider }

// contentsPath is the contents API path of path at ref.
func contentsPath(k core.Key, path, ref string) string {
	p := repoPath(k, "contents")
	if rest := escapePath(path); rest != "" {
		p += "/" + rest
	}
	return p + "?ref=" + url.QueryEscape(ref)
}

type contentJSON struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Size     int    `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

// read reads path at ref, which is a file object or a list of entries.
// found is false when GitHub has no such path and the repository can be
// read; when it cannot, the error says so: a 404 is not a missing file
// until the repository is known to be readable.
func (f files) read(ctx context.Context, op string, k core.Key, ref, path string) (body []byte, found bool, err error) {
	if err := f.p.checkHost(op, k); err != nil {
		return nil, false, err
	}
	resp, err := f.p.get(ctx, contentsPath(k, path, ref), "")
	if err != nil {
		err = f.p.fail(op, err)
		if isKind(err, base.ErrNotFound) {
			if rerr := f.p.readableRepo(ctx, k); rerr != nil {
				return nil, false, rerr
			}
			return nil, false, nil
		}
		return nil, false, err
	}
	return resp.Body, true, nil
}

// readableRepo returns why the repository of k cannot be read, or nil.
func (p *Provider) readableRepo(ctx context.Context, k core.Key) error {
	_, err := p.repoInfo(ctx, k)
	return err
}

// Get implements core.Files. A path that is not a file, such as a
// directory, is not found.
func (f files) Get(ctx context.Context, k core.Key, ref, path string) ([]byte, bool, error) {
	op := "file " + path + "@" + ref + " of " + k.ProjectKey()
	body, found, err := f.read(ctx, op, k, ref, path)
	if err != nil || !found {
		return nil, false, err
	}
	var c contentJSON
	if err := decodeObject(op, body, &c); err != nil {
		var list []contentJSON
		if decode(op, body, &list) == nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	if c.Type != "file" {
		return nil, false, nil
	}
	data, err := f.decodeFile(ctx, op, k, ref, path, c)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// decodeObject decodes a JSON object, and only an object.
func decodeObject(op string, body []byte, v any) error {
	if b := bytes.TrimSpace(body); len(b) == 0 || b[0] != '{' {
		return &base.Error{Kind: base.ErrTransport, Op: op, Err: errors.New("not an object")}
	}
	return decode(op, body, v)
}

// decodeFile returns the content of a file object: from its base64 body, or,
// for a file too large for the contents API to inline, from the raw media
// type. The content must be valid UTF-8.
func (f files) decodeFile(ctx context.Context, op string, k core.Key, ref, path string, c contentJSON) ([]byte, error) {
	var data []byte
	switch {
	case c.Encoding == "base64":
		var err error
		data, err = base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(c.Content))
		if err != nil {
			return nil, &base.Error{Kind: base.ErrTransport, Op: op, Err: errors.New("content is not valid base64")}
		}
	case c.Size == 0:
		data = []byte{}
	default:
		resp, err := f.p.get(ctx, contentsPath(k, path, ref), acceptRaw)
		if err != nil {
			return nil, f.p.fail(op, err)
		}
		data = resp.Body
	}
	if !utf8.Valid(data) {
		return nil, &base.Error{Kind: base.ErrTransport, Op: op, Err: errors.New("content is not valid UTF-8")}
	}
	if data == nil {
		data = []byte{}
	}
	return data, nil
}

// List implements core.Files. A directory that does not exist, or a path
// that is a file, has no entries.
func (f files) List(ctx context.Context, k core.Key, ref, dir string) ([]core.DirEntry, error) {
	op := "directory " + dir + "@" + ref + " of " + k.ProjectKey()
	body, found, err := f.read(ctx, op, k, ref, dir)
	if err != nil || !found {
		return nil, err
	}
	var list []contentJSON
	if err := decode(op, body, &list); err != nil {
		if decodeObject(op, body, &contentJSON{}) == nil {
			return nil, nil
		}
		return nil, err
	}
	out := make([]core.DirEntry, 0, len(list))
	for _, e := range list {
		out = append(out, core.DirEntry{Name: e.Name, Dir: e.Type == "dir"})
	}
	return out, nil
}
