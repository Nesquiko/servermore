-- name: FunctionById :one
select * from functions where id = ?;

-- name: RunnerByAddr :one
select * from runners where addr = ?;
