package store

import (
	"fmt"
	"slices"
)

// SetOCR records the text recognized in the image item with the given id
// and marks it done: the background recognizer never asks for the same
// image twice, whether it found text or not. Persisted atomically like
// SetPinned (memory rolls back on save failure). An unknown id — an item
// removed while its recognition was in flight — is an error, so callers
// drop the result rather than resurrecting it.
func (s *Store) SetOCR(id, text string) error {
	idx := slices.IndexFunc(s.Items, func(it Item) bool { return it.ID == id })
	if idx < 0 {
		return fmt.Errorf("store: no item with id %q", id)
	}
	if s.Items[idx].OCRDone && s.Items[idx].OCRText == text {
		return nil
	}
	old := s.Items[idx]
	s.Items[idx].OCRText = text
	s.Items[idx].OCRDone = true
	if err := s.Save(); err != nil {
		s.Items[idx] = old
		return err
	}
	return nil
}
