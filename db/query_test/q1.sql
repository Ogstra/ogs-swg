-- Admin Queries --
-- name: CreateAdmin :exec
INSERT INTO admins (username, password_hash) VALUES (?, ?);

-- name: GetAdmin :one
SELECT password_hash FROM admins WHERE username = ?;

-- name: UpdateAdminPassword :exec
UPDATE admins SET password_hash = ? WHERE username = ?;

-- name: CheckAdminExists :one
SELECT COUNT(*) FROM admins WHERE username = ?;

-- name: UpdateAdminUsername :exec
UPDATE admins SET username = ? WHERE username = ?;

-- name: CountAdmins :one
SELECT COUNT(*) FROM admins;

-- InboundMeta Queries --
