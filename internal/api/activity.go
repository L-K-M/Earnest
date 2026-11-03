package api

import (
	"context"
	"net/http"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
)

type activityInput struct {
	PrincipalID int64     `query:"principal_id"`
	Action      string    `query:"action"`
	Since       time.Time `query:"since"`
	AfterID     int64     `query:"after_id" minimum:"0"`
	Limit       int       `query:"limit" default:"100" minimum:"1" maximum:"1000"`
	Grouped     bool      `query:"grouped"`
}
type activityGroup struct {
	PrincipalID   int64              `json:"principal_id"`
	PrincipalName string             `json:"principal_name"`
	Activities    []service.Activity `json:"activities"`
}
type activityPage struct {
	Activities []service.Activity `json:"activities,omitempty"`
	Groups     []activityGroup    `json:"groups,omitempty"`
}
type agentChangesInput struct {
	Since   time.Time `query:"since" required:"true"`
	AfterID int64     `query:"after_id" minimum:"0"`
	Limit   int       `query:"limit" default:"1000" minimum:"1" maximum:"1000"`
}
type triageBody struct {
	OpID       string               `json:"op_id"`
	Generation string               `json:"generation"`
	EntryIDs   []int64              `json:"entry_ids"`
	Action     service.TriageAction `json:"action" enum:"seen,done"`
}
type triageRevertBody struct {
	RevertHandle string `json:"revert_handle"`
	OpID         string `json:"op_id"`
}

func (s *server) activityRoutes() {
	route(s, http.MethodGet, "/v1/activity", "list-activity", func(ctx context.Context, in *activityInput) (activityPage, error) {
		filter := service.ActivityFilter{PrincipalID: in.PrincipalID, Action: in.Action, Since: in.Since, AfterID: in.AfterID, Limit: in.Limit}
		if !in.Grouped {
			rows, err := s.svc.ListActivity(ctx, actor(ctx), filter)
			return activityPage{Activities: rows}, err
		}
		rows, err := s.svc.ActivityByPrincipal(ctx, actor(ctx), filter)
		if err != nil {
			return activityPage{}, err
		}
		groups := make([]activityGroup, 0, len(rows))
		for _, row := range rows {
			groups = append(groups, activityGroup{PrincipalID: row.PrincipalID, PrincipalName: row.PrincipalName, Activities: row.Activities})
		}
		return activityPage{Groups: groups}, nil
	})
	route(s, http.MethodPost, "/v1/activity/{id}:revert", "revert-activity", func(ctx context.Context, in *idBody[opBody]) (empty, error) {
		return empty{}, s.svc.Revert(ctx, actor(ctx), in.ID, in.Body.OpID)
	})
	route(s, http.MethodGet, "/v1/activity/agent-changes", "agent-changes", func(ctx context.Context, in *agentChangesInput) ([]service.Activity, error) {
		return s.svc.AgentChangesToUnseen(ctx, actor(ctx), in.Since, service.ActivityFilter{AfterID: in.AfterID, Limit: in.Limit})
	})
	route(s, http.MethodPost, "/v1/triage", "triage", func(ctx context.Context, in *bodyInput[triageBody]) (service.TriageResult, error) {
		b := in.Body
		return s.svc.Triage(ctx, actor(ctx), service.TriageRequest{OpID: b.OpID, Generation: b.Generation, EntryIDs: b.EntryIDs, Action: b.Action})
	})
	route(s, http.MethodPost, "/v1/triage:revert", "revert-triage", func(ctx context.Context, in *bodyInput[triageRevertBody]) (empty, error) {
		return empty{}, s.svc.RevertTriage(ctx, actor(ctx), in.Body.RevertHandle, in.Body.OpID)
	})
}
