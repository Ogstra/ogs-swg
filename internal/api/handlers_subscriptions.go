package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Ogstra/ogs-swg/internal/core/store"
)

// generateToken creates a random 32-character hex string
func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type SubscriptionResponse struct {
	ID        int64    `json:"id"`
	Token     string   `json:"token"`
	Name      string   `json:"name"`
	Users     []string `json:"users"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

func (s *Server) handleGetSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := s.store.Queries.GetAllSubscriptions(r.Context())
	if err != nil {
		http.Error(w, "Failed to get subscriptions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	res := make([]SubscriptionResponse, 0, len(subs))
	for _, sub := range subs {
		users, _ := s.store.Queries.GetUsersForSubscription(r.Context(), sub.ID)
		res = append(res, SubscriptionResponse{
			ID:        sub.ID,
			Token:     sub.Token,
			Name:      sub.Name,
			Users:     users,
			CreatedAt: sub.CreatedAt.Int64, // Wait, need to check if created_at is sql.NullInt64 or direct int64
			UpdatedAt: sub.UpdatedAt.Int64,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

type CreateSubscriptionRequest struct {
	Name  string   `json:"name"`
	Users []string `json:"users"`
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	var req CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	token := generateToken()
	id, err := s.store.Queries.CreateSubscription(r.Context(), store.CreateSubscriptionParams{
		Token: token,
		Name:  req.Name,
	})
	if err != nil {
		http.Error(w, "Failed to create subscription: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for _, user := range req.Users {
		_ = s.store.Queries.AddUserToSubscription(r.Context(), store.AddUserToSubscriptionParams{
			SubID:    id,
			UserName: user,
		})
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "token": token})
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	sub, err := s.store.Queries.GetSubscriptionByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Subscription not found", http.StatusNotFound)
		return
	}

	users, _ := s.store.Queries.GetUsersForSubscription(r.Context(), id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SubscriptionResponse{
		ID:        sub.ID,
		Token:     sub.Token,
		Name:      sub.Name,
		Users:     users,
		CreatedAt: sub.CreatedAt.Int64,
		UpdatedAt: sub.UpdatedAt.Int64,
	})
}

func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var req CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	err = s.store.Queries.UpdateSubscriptionName(r.Context(), store.UpdateSubscriptionNameParams{
		Name: req.Name,
		ID:   id,
	})
	if err != nil {
		http.Error(w, "Failed to update subscription", http.StatusInternalServerError)
		return
	}

	s.store.Queries.ClearSubscriptionUsers(r.Context(), id)
	for _, user := range req.Users {
		_ = s.store.Queries.AddUserToSubscription(r.Context(), store.AddUserToSubscriptionParams{
			SubID:    id,
			UserName: user,
		})
	}

	s.InvalidateSubCache()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	err = s.store.Queries.DeleteSubscription(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to delete subscription", http.StatusInternalServerError)
		return
	}

	s.InvalidateSubCache()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRegenerateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	token := generateToken()
	err = s.store.Queries.RegenerateSubscriptionToken(r.Context(), store.RegenerateSubscriptionTokenParams{
		Token: token,
		ID:    id,
	})
	if err != nil {
		http.Error(w, "Failed to regenerate token", http.StatusInternalServerError)
		return
	}

	s.InvalidateSubCache()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}
