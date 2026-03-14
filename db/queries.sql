-- name: CreateFunction :one
insert into functions (path, name) values (?, ?)
returning *;

