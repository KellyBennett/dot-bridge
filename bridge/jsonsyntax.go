package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// jsonWalker checks duplicate keys and bounded nesting without retaining values.
type jsonWalker struct{ decoder *json.Decoder }

func newJSONWalker(raw []byte) *jsonWalker {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return &jsonWalker{decoder: decoder}
}

func uniqueJSON(raw []byte) error {
	w := newJSONWalker(raw)
	if err := w.value(0); err != nil {
		return err
	}
	return w.finished()
}

func (w *jsonWalker) finished() error {
	if _, err := w.decoder.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func (w *jsonWalker) value(depth int) error {
	if depth > 32 {
		return errors.New("nesting limit")
	}
	token, err := w.decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := token.(json.Delim); ok {
		return w.container(delim, depth+1)
	}
	return nil
}

func (w *jsonWalker) container(delim json.Delim, depth int) error {
	switch delim {
	case '{':
		return w.object(depth)
	case '[':
		return w.array(depth)
	default:
		return errors.New("unexpected delimiter")
	}
}

func (w *jsonWalker) end() error {
	_, err := w.decoder.Token()
	return err
}

func (w *jsonWalker) array(depth int) error {
	for w.decoder.More() {
		if err := w.value(depth); err != nil {
			return err
		}
	}
	return w.end()
}

func (w *jsonWalker) object(depth int) error {
	seen := map[string]bool{}
	for w.decoder.More() {
		if err := w.key(seen); err != nil {
			return err
		}
		if err := w.value(depth); err != nil {
			return err
		}
	}
	return w.end()
}

func (w *jsonWalker) key(seen map[string]bool) error {
	token, err := w.decoder.Token()
	if err != nil {
		return err
	}
	name, ok := token.(string)
	if !ok || seen[name] {
		return errors.New("duplicate or invalid key")
	}
	seen[name] = true
	return nil
}
