package service

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestClassifyMediaCategoryMatchesMoviePilotStyleRules(t *testing.T) {
	tests := []struct {
		name  string
		input mediaClassifyInput
		want  string
	}{
		{
			name: "movie animation first",
			input: mediaClassifyInput{
				MediaType: "movie",
				Title:     "Robot Dreams",
				Countries: []string{"ES"},
				Genres:    []string{"Animation"},
			},
			want: "动画电影",
		},
		{
			name: "tv variety by genre",
			input: mediaClassifyInput{
				MediaType: "tv",
				Title:     "声生不息",
				Countries: []string{"CN"},
				Genres:    []string{"Reality"},
			},
			want: "综艺",
		},
		{
			name: "anime china",
			input: mediaClassifyInput{
				MediaType: "anime",
				Countries: []string{"CN"},
				Genres:    []string{"Animation"},
			},
			want: "国漫",
		},
		{
			name: "tv documentary before region",
			input: mediaClassifyInput{
				MediaType: "tv",
				Countries: []string{"US"},
				Genres:    []string{"Documentary"},
			},
			want: "纪录片",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyMediaCategory(tt.input, nil); got != tt.want {
				t.Fatalf("category = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubscriptionResolveClassifiedSavePath(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), "organizer.smart_classify", "true"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "qbittorrent.savepath", filepath.Join("D:", "Downloads")); err != nil {
		t.Fatal(err)
	}
	svc := NewSubscriptionService(&config.Config{}, zap.NewNop(), repos, nil, nil, nil)
	sub := &model.Subscription{Name: "声生不息 自动订阅", MediaType: "tv"}

	mediaType, category := svc.classifySubscriptionItem(t.Context(), sub, "声生不息 S01E01", "综艺")
	if mediaType != "tv" || category != "综艺" {
		t.Fatalf("classification = %q/%q, want tv/综艺", mediaType, category)
	}
	got := svc.resolveSubscriptionSavePath(t.Context(), sub, mediaType, category)
	want := filepath.Join("D:", "Downloads", "综艺")
	if got != want {
		t.Fatalf("save path = %q, want %q", got, want)
	}
}
