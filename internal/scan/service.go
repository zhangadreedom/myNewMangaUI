package scan

import "context"

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Scan(ctx context.Context) error {
	_ = ctx
	return nil
}
