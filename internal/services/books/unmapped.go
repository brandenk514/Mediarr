package books

import (
	"context"
	"fmt"
	"strings"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
)

// MediaRoot returns the configured books media root (the default import
// location; the Readarr B3 "Unmapped Files" walk root).
func (s *Service) MediaRoot() string {
	return s.deps.MediaRoot
}

// ListUnmappedFiles (Readarr B3, the "Unmapped Files" page) lists the book
// files under root that are not mapped to a monitored author. It loads the
// tracked authors, collects the monitored names, and delegates the
// filesystem walk to the pure domain scan (dom.ScanUnmappedFiles).
func (s *Service) ListUnmappedFiles(ctx context.Context, root string) ([]dom.UnmappedFile, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("books: unmapped files: root folder path is required")
	}
	authors, err := s.deps.Repo.ListAuthors(ctx)
	if err != nil {
		return nil, err
	}
	monitored := map[string]bool{}
	for _, a := range authors {
		if a.Monitored {
			monitored[a.Name] = true
		}
	}
	return dom.ScanUnmappedFiles(root, monitored)
}
