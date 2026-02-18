package image

import "context"

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) WarmCache(ctx context.Context) error {
	_ = ctx
	return nil
}
