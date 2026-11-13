package service

import (
	"context"
	"io"
	"net/http"
	"time"

	"hitall.com/middle-platform/sdk-service/internal/auth"
	"hitall.com/middle-platform/sdk-service/internal/model"
	"hitall.com/middle-platform/shared/logging"
)

type UserCheckSender interface {
	CheckUser(context.Context, string, string, []byte) (bool, error)
}

type UserCheckRouteReader interface {
	Lookup(context.Context, string, string) (model.Target, error)
}

type UserCheckService struct {
	Routes UserCheckRouteReader
	Sender UserCheckSender
}

func (s *UserCheckService) Check(r *http.Request, env, appID string, _ model.UserCheckRequest) (model.UserCheckResult, error) {
	target, err := s.Routes.Lookup(r.Context(), appID, env)
	if err != nil {
		return model.UserCheckResult{}, err
	}
	if target.AppID != appID || target.Env != env {
		return model.UserCheckResult{}, model.ErrInvalidConfig
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return model.UserCheckResult{}, err
	}
	if err := auth.VerifyGameRequest(r, appID, target.GameSecret, body, time.Now()); err != nil {
		return model.UserCheckResult{}, err
	}
	if target.SkipUserCheck {
		logging.FromContext(r.Context()).InfoContext(r.Context(), "已跳过平台用户核验", "app_id", appID, "env", env)
		return model.UserCheckResult{Exists: true}, nil
	}
	exists, err := s.Sender.CheckUser(r.Context(), appID, target.AppSecret, body)
	return model.UserCheckResult{Exists: exists}, err
}
