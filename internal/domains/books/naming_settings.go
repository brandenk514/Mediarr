// Package books (Readarr B5) — settings → naming-engine bridge.
//
// This file maps the Books media-management naming CONFIG (settings.BookNaming)
// into the naming engine's options (domains/books.NamingOptions) and offers a
// single helper that renders a book's file path / author folder from a config
// + a naming context. It keeps the engine (naming.go) decoupled from the
// settings types: the engine is a pure function over NamingOptions, and this
// bridge is the one place that knows both shapes.
package books

import (
	"github.com/brandenk514/mediarr/internal/settings"
)

// NamingOptionsFromBookNaming maps a books naming config into the engine's
// options. The two template fields (standard_format / author_format) stay on
// the config — they are passed to the render helpers as separate arguments —
// while the five behavioural knobs map 1:1.
func NamingOptionsFromBookNaming(bn settings.BookNaming) NamingOptions {
	return NamingOptions{
		RenameBooks:              bn.RenameBooks,
		ReplaceIllegalCharacters: bn.ReplaceIllegalCharacters,
		ColonReplacement:         bn.ColonReplacement,
		SpaceHandling:            bn.SpaceHandling,
		CaseHandling:             bn.CaseHandling,
	}
}

// RenderBookPath renders a book's file path (relative, no extension) from a
// books naming config and a naming context. It applies the config's
// standard_format template through the engine. An empty standard format is a
// config error (the settings Validate() prevents it, but the engine errors
// defensively).
func RenderBookPath(bn settings.BookNaming, c BookNamingContext) (string, error) {
	return RenderBookFileName(NamingOptionsFromBookNaming(bn), bn.StandardFormat, c)
}

// RenderBookAuthorFolder renders the author folder path (relative) from a
// books naming config and a naming context, using the config's author_format.
func RenderBookAuthorFolder(bn settings.BookNaming, c BookNamingContext) (string, error) {
	return RenderAuthorFolder(NamingOptionsFromBookNaming(bn), bn.AuthorFormat, c)
}
