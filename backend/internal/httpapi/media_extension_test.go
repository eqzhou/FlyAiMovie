package httpapi

import "testing"

// Uploaded and probed media are stored under an extension derived from the
// declared MIME type or the probed container, never from the client-supplied
// filename. An empty return means "not an accepted media type" and the caller
// rejects the file, so both the mapping and the rejection path matter.

func TestCanonicalMediaUploadExtension(t *testing.T) {
	cases := map[string]string{
		"video/mp4":             ".mp4",
		"video/webm":            ".webm",
		"video/quicktime":       ".mov",
		"video/avi":             ".avi",
		"video/x-msvideo":       ".avi",
		"video/x-matroska":      ".mkv",
		"video/mpeg":            ".mpeg",
		"video/ogg":             ".ogv",
		"audio/mpeg":            ".mp3",
		"audio/wav":             ".wav",
		"audio/wave":            ".wav",
		"audio/x-wav":           ".wav",
		"audio/ogg":             ".ogg",
		"application/ogg":       ".ogg",
		"audio/webm":            ".weba",
		"audio/mp4":             ".m4a",
		"audio/x-m4a":           ".m4a",
		"audio/aac":             ".aac",
		"audio/flac":            ".flac",
		"audio/x-flac":          ".flac",
		"audio/aiff":            ".aiff",
		"audio/midi":            ".mid",
		"  VIDEO/MP4  ":         ".mp4",
		"image/png":             "",
		"text/html":             "",
		"application/xhtml":     "",
		"":                      "",
		"video/mp4; codecs=av1": "",
	}

	for mime, want := range cases {
		if got := canonicalMediaUploadExtension(mime); got != want {
			t.Errorf("canonicalMediaUploadExtension(%q) = %q, want %q", mime, got, want)
		}
	}
}

func TestCanonicalMediaProbeExtension(t *testing.T) {
	cases := []struct {
		format    string
		assetType string
		want      string
	}{
		// The mp4 family is shared between audio and video, so the asset type
		// decides whether it lands as .mp4 or .m4a.
		{format: "mp4", assetType: "video", want: ".mp4"},
		{format: "mp4", assetType: "audio", want: ".m4a"},
		{format: "mov", assetType: "video", want: ".mp4"},
		{format: "m4v", assetType: "audio", want: ".m4a"},
		{format: "3gp", assetType: "video", want: ".mp4"},
		{format: "3g2", assetType: "audio", want: ".m4a"},
		{format: "mj2", assetType: "video", want: ".mp4"},
		{format: "matroska", assetType: "video", want: ".mkv"},
		{format: "webm", assetType: "video", want: ".webm"},
		{format: "avi", assetType: "video", want: ".avi"},
		{format: "mpeg", assetType: "video", want: ".mpeg"},
		{format: "mpegts", assetType: "video", want: ".mpeg"},
		{format: "m2ts", assetType: "video", want: ".mpeg"},
		{format: "ogg", assetType: "video", want: ".ogv"},
		{format: "ogg", assetType: "audio", want: ".ogg"},
		{format: "ogv", assetType: "video", want: ".ogv"},
		{format: "mp3", assetType: "audio", want: ".mp3"},
		{format: "wav", assetType: "audio", want: ".wav"},
		{format: "aiff", assetType: "audio", want: ".aiff"},
		{format: "flac", assetType: "audio", want: ".flac"},
		{format: "aac", assetType: "audio", want: ".aac"},
		{format: "midi", assetType: "audio", want: ".mid"},
		{format: "mid", assetType: "audio", want: ".mid"},
		// ffprobe reports comma-separated candidates; the first one wins.
		{format: "mov,mp4,m4a,3gp", assetType: "video", want: ".mp4"},
		{format: "matroska,webm", assetType: "video", want: ".mkv"},
		{format: "  MP4  ", assetType: "video", want: ".mp4"},
		{format: "image2", assetType: "video", want: ""},
		{format: "", assetType: "video", want: ""},
	}

	for _, tc := range cases {
		if got := canonicalMediaProbeExtension(tc.format, tc.assetType); got != tc.want {
			t.Errorf("canonicalMediaProbeExtension(%q, %q) = %q, want %q",
				tc.format, tc.assetType, got, tc.want)
		}
	}
}

func TestOptionalFormID(t *testing.T) {
	// Absent, zero, and malformed values all mean "no id"; only a positive
	// integer produces a pointer, since 0 is not a valid row id.
	for _, raw := range []string{"", "  ", "0", "-1", "abc", "1.5", "99999999999999999999"} {
		if got := optionalFormID(raw); got != nil {
			t.Errorf("optionalFormID(%q) = %v, want nil", raw, *got)
		}
	}
	got := optionalFormID(" 42 ")
	if got == nil {
		t.Fatal("optionalFormID(\" 42 \") = nil, want 42")
	}
	if *got != 42 {
		t.Fatalf("optionalFormID(\" 42 \") = %d, want 42", *got)
	}
}
