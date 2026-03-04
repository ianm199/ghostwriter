package transcribe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Store struct {
	dir string
}

type SearchResult struct {
	ID      string
	Title   string
	Snippet string
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Write(t *Transcript) error {
	t.Version = "1.0"

	year := t.Metadata.Date.Format("2006")
	month := t.Metadata.Date.Format("01")
	dir := filepath.Join(s.dir, year, month)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := filepath.Join(dir, t.ID+".transcript.json")
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func WriteTranscript(t *Transcript, path string) error {
	t.Version = "1.0"
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *Store) Get(id string) (*Transcript, error) {
	path, err := s.findByID(id)
	if err != nil {
		return nil, err
	}
	return s.readFile(path)
}

func (s *Store) List(since time.Time) ([]*Transcript, error) {
	var results []*Transcript

	err := filepath.Walk(s.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !strings.HasSuffix(path, ".transcript.json") {
			return nil
		}

		t, err := s.readFile(path)
		if err != nil {
			return nil
		}

		if !since.IsZero() && t.Metadata.Date.Before(since) {
			return nil
		}

		results = append(results, t)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Metadata.Date.After(results[j].Metadata.Date)
	})

	return results, nil
}

func (s *Store) Search(query string) ([]SearchResult, error) {
	query = strings.ToLower(query)
	var results []SearchResult

	err := filepath.Walk(s.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !strings.HasSuffix(path, ".transcript.json") {
			return nil
		}

		t, err := s.readFile(path)
		if err != nil {
			return nil
		}

		lower := strings.ToLower(t.FullText)
		idx := strings.Index(lower, query)
		if idx == -1 {
			return nil
		}

		start := idx - 40
		if start < 0 {
			start = 0
		}
		end := idx + len(query) + 40
		if end > len(t.FullText) {
			end = len(t.FullText)
		}

		results = append(results, SearchResult{
			ID:      t.ID,
			Title:   t.Metadata.Title,
			Snippet: t.FullText[start:end],
		})
		return nil
	})

	return results, err
}

func (s *Store) findByID(id string) (string, error) {
	var found string
	err := filepath.Walk(s.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.Contains(filepath.Base(path), id) && strings.HasSuffix(path, ".transcript.json") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("transcript %q not found", id)
	}
	return found, nil
}

func (s *Store) readFile(path string) (*Transcript, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Transcript
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
