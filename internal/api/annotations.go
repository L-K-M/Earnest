package api

import (
	"context"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

type annotationInput struct {
	ID          int64                  `path:"id" minimum:"1"`
	Kind        service.AnnotationKind `path:"kind" enum:"note,label,score,brief,summary,blurb"`
	PrincipalID int64                  `query:"principal_id" doc:"Author; defaults to the authenticated principal."`
}
type annotationPutInput struct {
	ID   int64                  `path:"id" minimum:"1"`
	Kind service.AnnotationKind `path:"kind" enum:"note,label,score,brief,summary,blurb"`
	Body annotationBody
}
type annotationDeleteInput struct {
	ID   int64                  `path:"id" minimum:"1"`
	Kind service.AnnotationKind `path:"kind" enum:"note,label,score,brief,summary,blurb"`
	OpID string                 `query:"op_id" required:"true"`
}
type annotationBody struct {
	Value string `json:"value"`
	OpID  string `json:"op_id"`
}
type briefBody struct {
	EntryIDs []int64 `json:"entry_ids"`
	Value    string  `json:"value"`
	OpID     string  `json:"op_id"`
}
type annotationsInput struct {
	EntryIDs       []int64                `query:"entry_ids"`
	PrincipalID    int64                  `query:"principal_id"`
	Kind           service.AnnotationKind `query:"kind" enum:"note,label,score,brief,summary,blurb"`
	IncludeDeleted bool                   `query:"include_deleted"`
	AfterID        int64                  `query:"after_id" minimum:"0"`
	Limit          int                    `query:"limit" default:"100" minimum:"1" maximum:"1000"`
}

func (s *server) annotationRoutes() {
	route(s, http.MethodGet, "/v1/entries/{id}/annotations/{kind}", "get-annotation", func(ctx context.Context, in *annotationInput) (service.Annotation, error) {
		principal := in.PrincipalID
		if principal == 0 {
			principal = actor(ctx).PrincipalID
		}
		return s.svc.GetAnnotation(ctx, actor(ctx), principal, []int64{in.ID}, in.Kind)
	})
	route(s, http.MethodPut, "/v1/entries/{id}/annotations/{kind}", "put-annotation", func(ctx context.Context, in *annotationPutInput) (service.Annotation, error) {
		return s.svc.PutAnnotation(ctx, actor(ctx), service.PutAnnotationRequest{EntryIDs: []int64{in.ID}, Kind: in.Kind, Value: in.Body.Value, OpID: in.Body.OpID})
	})
	route(s, http.MethodDelete, "/v1/entries/{id}/annotations/{kind}", "delete-annotation", func(ctx context.Context, in *annotationDeleteInput) (empty, error) {
		return empty{}, s.svc.DeleteAnnotation(ctx, actor(ctx), []int64{in.ID}, in.Kind, in.OpID)
	})
	route(s, http.MethodGet, "/v1/annotations", "list-annotations", func(ctx context.Context, in *annotationsInput) ([]service.Annotation, error) {
		return s.svc.ListAnnotations(ctx, actor(ctx), service.AnnotationQuery{EntryIDs: in.EntryIDs, PrincipalID: in.PrincipalID, Kind: in.Kind, IncludeDeleted: in.IncludeDeleted, AfterID: in.AfterID, Limit: in.Limit})
	})
	route(s, http.MethodPost, "/v1/briefs", "create-brief", func(ctx context.Context, in *bodyInput[briefBody]) (service.Annotation, error) {
		return s.svc.PutAnnotation(ctx, actor(ctx), service.PutAnnotationRequest{EntryIDs: in.Body.EntryIDs, Kind: service.AnnotationBrief, Value: in.Body.Value, OpID: in.Body.OpID})
	})
	route(s, http.MethodGet, "/v1/briefs/today", "today-briefs", func(ctx context.Context, in *empty) ([]service.Annotation, error) {
		return s.svc.TodayBriefs(ctx, actor(ctx))
	})
}
