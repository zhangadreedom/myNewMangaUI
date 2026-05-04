package online

import (
	"fmt"
	"strings"

	"mynewmangaui/internal/config"
)

func NewDefaultService(cfg config.OnlineConfig) (*Service, error) {
	providers := make([]Provider, 0, len(cfg.Sources))

	for _, source := range cfg.Sources {
		switch strings.TrimSpace(source.ID) {
		case "18comic":
			provider, err := New18ComicProvider(cfg, source)
			if err != nil {
				return nil, fmt.Errorf("create 18comic provider: %w", err)
			}
			providers = append(providers, provider)
		case "ehentai":
			provider, err := NewEHentaiProvider(cfg, source)
			if err != nil {
				return nil, fmt.Errorf("create ehentai provider: %w", err)
			}
			providers = append(providers, provider)
		case "pica":
			provider, err := NewPicaProvider(cfg, source)
			if err != nil {
				return nil, fmt.Errorf("create pica provider: %w", err)
			}
			providers = append(providers, provider)
		}
	}

	return NewService(cfg, providers...), nil
}
