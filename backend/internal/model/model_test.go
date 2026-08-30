package model

import "testing"

func TestMapCategory(t *testing.T) {
	tests := []struct {
		input  string
		expect GalleryCategory
	}{
		{"doujinshi", CategoryDoujinshi},
		{"Doujinshi", CategoryDoujinshi},
		{"manga", CategoryManga},
		{"Manga", CategoryManga},
		{"artist cg", CategoryArtistCG},
		{"Artist CG", CategoryArtistCG},
		{"game cg", CategoryGameCG},
		{"western", CategoryWestern},
		{"image set", CategoryImageSet},
		{"cosplay", CategoryCosplay},
		{"asian porn", CategoryAsianPorn},
		{"non-h", CategoryNonH},
		{"miscellaneous", CategoryMisc},
		{"unknown", CategoryOther},
		{"", CategoryOther},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := MapCategory(tt.input)
			if got != tt.expect {
				t.Errorf("MapCategory(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestParseTags(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect []Tag
	}{
		{
			"namespace:tag format",
			[]string{"female:yuri", "male:solemale"},
			[]Tag{
				{Namespace: "female", Name: "yuri"},
				{Namespace: "male", Name: "solemale"},
			},
		},
		{
			"no namespace",
			[]string{"uncensored"},
			[]Tag{
				{Namespace: "", Name: "uncensored"},
			},
		},
		{
			"mixed",
			[]string{"female:yuri", "uncensored", "language:chinese"},
			[]Tag{
				{Namespace: "female", Name: "yuri"},
				{Namespace: "", Name: "uncensored"},
				{Namespace: "language", Name: "chinese"},
			},
		},
		{
			"empty",
			[]string{},
			[]Tag{},
		},
		{
			"nil",
			nil,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTags(tt.input)
			if len(got) != len(tt.expect) {
				t.Fatalf("ParseTags(%v) returned %d tags, want %d", tt.input, len(got), len(tt.expect))
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Errorf("ParseTags(%v)[%d] = %v, want %v", tt.input, i, got[i], tt.expect[i])
				}
			}
		})
	}
}

const (
	testGalleryID    = 4153369
	testGalleryToken = "51d1aa689c"
)

func TestConvertMetadataToGallery(t *testing.T) {
	posted := "1609459200" // 2021-01-01 00:00:00 UTC
	meta := &GalleryMetadata{
		GID:          testGalleryID,
		Token:        testGalleryToken,
		Title:        "Test Gallery",
		TitleJpn:     "テストギャラリー",
		Category:     "Manga",
		Thumb:        "https://example.com/thumb.jpg",
		Uploader:     "uploader1",
		Posted:       posted,
		FileCount:    "65",
		FileSize:     1048576,
		Expunged:     false,
		Rating:       "4.86",
		TorrentCount: "3",
		Tags:         []string{"female:yuri", "language:chinese"},
	}

	gallery := ConvertMetadataToGallery(meta)

	if gallery.ID != testGalleryID {
		t.Errorf("ID = %d, want %d", gallery.ID, testGalleryID)
	}
	if gallery.Token != testGalleryToken {
		t.Errorf("Token = %q, want %q", gallery.Token, testGalleryToken)
	}
	if gallery.Title != "Test Gallery" {
		t.Errorf("Title = %q, want %q", gallery.Title, "Test Gallery")
	}
	if gallery.TitleJPN != "テストギャラリー" {
		t.Errorf("TitleJPN = %q, want %q", gallery.TitleJPN, "テストギャラリー")
	}
	if gallery.Category != CategoryManga {
		t.Errorf("Category = %q, want %q", gallery.Category, CategoryManga)
	}
	if gallery.PageCount != 65 {
		t.Errorf("PageCount = %d, want 65", gallery.PageCount)
	}
	if gallery.Rating != 4.86 {
		t.Errorf("Rating = %f, want 4.86", gallery.Rating)
	}
	if gallery.PostedAt == nil {
		t.Fatal("PostedAt is nil")
	}
	if gallery.PostedAt.Year() != 2021 {
		t.Errorf("PostedAt.Year() = %d, want 2021", gallery.PostedAt.Year())
	}
	if len(gallery.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(gallery.Tags))
	}
	if gallery.Expunged {
		t.Error("Expunged should be false")
	}
}

func TestConvertMetadataToGallery_InvalidRating(t *testing.T) {
	meta := &GalleryMetadata{
		GID:          123,
		Token:        "abc",
		Title:        "Test",
		Rating:       "not-a-number",
		FileCount:    "not-a-number",
		Posted:       "not-a-number",
		FileSize:     0,
		TorrentCount: "0",
	}
	gallery := ConvertMetadataToGallery(meta)
	if gallery.Rating != 0 {
		t.Errorf("Rating should default to 0 for invalid input, got %f", gallery.Rating)
	}
	if gallery.PageCount != 0 {
		t.Errorf("PageCount should default to 0 for invalid input, got %d", gallery.PageCount)
	}
	if gallery.PostedAt != nil {
		t.Error("PostedAt should be nil for invalid timestamp")
	}
}

func BenchmarkMapCategory(b *testing.B) {
	for b.Loop() {
		MapCategory("Doujinshi")
	}
}

func BenchmarkConvertMetadataToGallery(b *testing.B) {
	meta := &GalleryMetadata{
		GID:          testGalleryID,
		Token:        testGalleryToken,
		Title:        "Benchmark Gallery",
		Category:     "Manga",
		Posted:       "1609459200",
		FileCount:    "65",
		Rating:       "4.86",
		TorrentCount: "3",
		Tags:         []string{"female:yuri", "language:chinese"},
	}
	for b.Loop() {
		ConvertMetadataToGallery(meta)
	}
}
