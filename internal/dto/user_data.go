package dto

// DeleteUserDataResponse is returned by DELETE /api/v1/me/data.
type DeleteUserDataResponse struct {
	DeletedAt string `json:"deleted_at"`
}
