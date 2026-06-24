package handler

import (
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestDiscoverProviderEnabledHonorsAPIConfigToggle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.APIConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	apiConfig := service.NewAPIConfigService(zap.NewNop(), repos, service.NewCryptoService("", zap.NewNop()))
	enabled := false
	if _, err := apiConfig.Update(t.Context(), "douban", service.APIConfigPatch{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{APIConfig: apiConfig}

	if discoverProviderEnabled(t.Context(), svc, "douban") {
		t.Fatal("disabled API config should disable discover provider")
	}
	if !discoverProviderEnabled(t.Context(), svc, "missing-provider") {
		t.Fatal("missing API config should keep discover provider available")
	}
}

func TestDefaultDiscoverSectionKeysSkipDisabledProviders(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.APIConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	apiConfig := service.NewAPIConfigService(zap.NewNop(), repos, service.NewCryptoService("", zap.NewNop()))
	disabled := false
	for _, provider := range []string{"douban", "bangumi"} {
		if _, err := apiConfig.Update(t.Context(), provider, service.APIConfigPatch{Enabled: &disabled}); err != nil {
			t.Fatal(err)
		}
	}
	svc := &service.Container{APIConfig: apiConfig}

	keys := defaultDiscoverSectionKeys(t.Context(), svc)
	for _, key := range keys {
		switch discoverSectionProvider(key) {
		case "douban", "bangumi":
			t.Fatalf("disabled provider key %q should not be selected by default; keys=%v", key, keys)
		}
	}
	if len(keys) == 0 {
		t.Fatal("default keys should keep enabled providers")
	}
}
