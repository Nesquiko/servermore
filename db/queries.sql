-- name: CreateFunction :one
insert into functions (path, name) values (?, ?)
returning *;

-- name: FunctionExistsByHash :one
select exists(select 1 from functions where hash = ?);
