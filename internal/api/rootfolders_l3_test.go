package api

import (
	"strings"
	"testing"
)

// L3 (Lidarr parity phase): the per-root default profile validation that the
// root-folder POST/PUT handlers apply (400 with a clear message on unknown
// names).
func TestL3_ValidateRootFolderRequest(t *testing.T) {
	cases := []struct {
		name    string
		req     rootFolderRequest
		wantErr string // "" = no error
	}{
		{"unset profiles allowed", rootFolderRequest{Path: "/m"}, ""},
		{
			"valid quality + metadata names",
			rootFolderRequest{Path: "/m", DefaultQualityProfile: "Lossless Best Effort", DefaultMetadataProfile: "Album"},
			"",
		},
		{
			"all 4 quality names accepted",
			rootFolderRequest{Path: "/m", DefaultQualityProfile: "WAV", DefaultMetadataProfile: "Standard"},
			"",
		},
		{
			"unknown quality profile",
			rootFolderRequest{Path: "/m", DefaultQualityProfile: "FLAC"},
			"unknown default quality profile",
		},
		{
			"unknown metadata profile",
			rootFolderRequest{Path: "/m", DefaultQualityProfile: "WAV", DefaultMetadataProfile: "Cuesheet"},
			"unknown default metadata profile",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateRootFolderRequest(tc.req)
			if tc.wantErr == "" {
				if msg != "" {
					t.Fatalf("validate = %q, want no error", msg)
				}
				return
			}
			if msg == "" {
				t.Fatal("expected a validation error, got none")
			}
			if !strings.Contains(msg, tc.wantErr) {
				t.Fatalf("validate = %q, want it to contain %q", msg, tc.wantErr)
			}
		})
	}
}
