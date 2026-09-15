package promotionapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"cobraknowledge.local/cobra-knowledge/internal/promotion"
)

type Handler struct { Service *promotion.Service; Token string }
func (h *Handler) Wrap(next http.Handler)http.Handler{mux:=http.NewServeMux();mux.HandleFunc("GET /healthz",h.health);mux.HandleFunc("POST /api/v1/promotion/candidates",h.createCandidate);mux.HandleFunc("GET /api/v1/promotion/candidates",h.listCandidates);mux.HandleFunc("GET /api/v1/promotion/candidates/{candidateID}",h.getCandidate);mux.HandleFunc("POST /api/v1/promotion/candidates/{candidateID}/check",h.recheckCandidate);mux.HandleFunc("POST /api/v1/promotion/candidates/{candidateID}/review",h.reviewCandidate);mux.HandleFunc("POST /api/v1/promotion/candidates/{candidateID}/publish",h.publishCandidate);mux.HandleFunc("POST /api/v1/promotion/candidates/{candidateID}/rollback",h.rollbackCandidate);mux.Handle("/",next);return mux}
func (h *Handler) health(w http.ResponseWriter,_ *http.Request){writeJSON(w,http.StatusOK,map[string]interface{}{"status":"ok","version":"0.10.1","promotion_enabled":h.Service!=nil&&strings.TrimSpace(h.Token)!=""})}
func (h *Handler) createCandidate(w http.ResponseWriter,r *http.Request){if !h.requirePromotion(w,r){return};var in promotion.CandidateInput;if err:=decodeJSON(r,&in);err!=nil{writeError(w,http.StatusBadRequest,err.Error());return};c,err:=h.Service.Create(r.Context(),principalFrom(r),in);if err!=nil{writePromotionError(w,err);return};writeJSON(w,http.StatusCreated,c)}
func (h *Handler) listCandidates(w http.ResponseWriter,r *http.Request){if !h.requirePromotion(w,r){return};limit:=100;if raw:=strings.TrimSpace(r.URL.Query().Get("limit"));raw!=""{if n,err:=strconv.Atoi(raw);err==nil&&n>0{limit=n}};items,err:=h.Service.List(r.Context(),principalFrom(r),limit);if err!=nil{writePromotionError(w,err);return};writeJSON(w,http.StatusOK,map[string]interface{}{"items":items})}
func (h *Handler) getCandidate(w http.ResponseWriter,r *http.Request){if !h.requirePromotion(w,r){return};c,err:=h.Service.Get(r.Context(),principalFrom(r),r.PathValue("candidateID"));if err!=nil{writePromotionError(w,err);return};writeJSON(w,http.StatusOK,c)}
func (h *Handler) recheckCandidate(w http.ResponseWriter,r *http.Request){if !h.requirePromotion(w,r){return};c,err:=h.Service.Recheck(r.Context(),principalFrom(r),r.PathValue("candidateID"));if err!=nil{writePromotionError(w,err);return};writeJSON(w,http.StatusOK,c)}
func (h *Handler) reviewCandidate(w http.ResponseWriter,r *http.Request){if !h.requirePromotion(w,r){return};var in promotion.ReviewInput;if err:=decodeJSON(r,&in);err!=nil{writeError(w,http.StatusBadRequest,err.Error());return};c,err:=h.Service.Review(r.Context(),principalFrom(r),r.PathValue("candidateID"),in);if err!=nil{writePromotionError(w,err);return};writeJSON(w,http.StatusOK,c)}
func (h *Handler) publishCandidate(w http.ResponseWriter,r *http.Request){if !h.requirePromotion(w,r){return};c,err:=h.Service.Publish(r.Context(),principalFrom(r),r.PathValue("candidateID"));if err!=nil{writePromotionError(w,err);return};writeJSON(w,http.StatusOK,c)}
func (h *Handler) rollbackCandidate(w http.ResponseWriter,r *http.Request){if !h.requirePromotion(w,r){return};var in promotion.RollbackInput;if r.ContentLength!=0{if err:=decodeJSON(r,&in);err!=nil{writeError(w,http.StatusBadRequest,err.Error());return}};c,err:=h.Service.Rollback(r.Context(),principalFrom(r),r.PathValue("candidateID"),in);if err!=nil{writePromotionError(w,err);return};writeJSON(w,http.StatusOK,c)}
func (h *Handler) requirePromotion(w http.ResponseWriter,r *http.Request)bool{if h.Service==nil||strings.TrimSpace(h.Token)==""{writeError(w,http.StatusServiceUnavailable,"promotion API is disabled");return false};expected:=strings.TrimSpace(h.Token);auth:=strings.TrimSpace(r.Header.Get("Authorization"));const prefix="Bearer ";if !strings.HasPrefix(auth,prefix){writeError(w,http.StatusUnauthorized,"promotion authentication required");return false};actual:=strings.TrimSpace(strings.TrimPrefix(auth,prefix));if len(actual)!=len(expected)||subtle.ConstantTimeCompare([]byte(actual),[]byte(expected))!=1{writeError(w,http.StatusUnauthorized,"promotion authentication required");return false};return true}
func principalFrom(r *http.Request)promotion.Principal{return promotion.Principal{WorkspaceID:strings.TrimSpace(r.Header.Get("X-LeeClaw-Workspace-ID")),UserID:strings.TrimSpace(r.Header.Get("X-LeeClaw-User-ID")),Role:strings.TrimSpace(r.Header.Get("X-LeeClaw-Workspace-Role")),TenantID:strings.TrimSpace(r.Header.Get("X-LeeClaw-WeKnora-Tenant-ID")),WeKnoraAPIKey:strings.TrimSpace(r.Header.Get("X-LeeClaw-WeKnora-API-Key")),WeKnoraBaseURL:strings.TrimSpace(r.Header.Get("X-LeeClaw-WeKnora-Base-URL")),OpenVikingAccountID:strings.TrimSpace(r.Header.Get("X-LeeClaw-OpenViking-Account")),KnowledgeBaseIDs:splitCSV(r.Header.Get("X-LeeClaw-Knowledge-Base-IDs"))}}
func splitCSV(value string)[]string{seen:=map[string]bool{};out:=[]string{};for _,v:=range strings.Split(value,","){v=strings.TrimSpace(v);if v!=""&&!seen[v]{seen[v]=true;out=append(out,v)}};return out}
func decodeJSON(r *http.Request,out interface{})error{dec:=json.NewDecoder(io.LimitReader(r.Body,4<<20));dec.DisallowUnknownFields();if err:=dec.Decode(out);err!=nil{return fmt.Errorf("invalid json body: %w",err)};return nil}
func writePromotionError(w http.ResponseWriter,err error){switch{case errors.Is(err,promotion.ErrCandidateNotFound):writeError(w,http.StatusNotFound,err.Error());case errors.Is(err,promotion.ErrAccessDenied):writeError(w,http.StatusForbidden,err.Error());case errors.Is(err,promotion.ErrVersionConflict),errors.Is(err,promotion.ErrPromotionBlocked),errors.Is(err,promotion.ErrInvalidTransition):writeError(w,http.StatusConflict,err.Error());default:writeError(w,http.StatusBadRequest,err.Error())}}
func writeError(w http.ResponseWriter,status int,message string){writeJSON(w,status,map[string]interface{}{"error":message})}
func writeJSON(w http.ResponseWriter,status int,value interface{}){w.Header().Set("Content-Type","application/json; charset=utf-8");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(value)}
