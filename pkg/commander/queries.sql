-- name: CreateFunction :one
insert into functions (path, name, hash) values (?, ?, ?)
returning *;

-- name: FunctionExistsByHash :one
select exists(select 1 from functions where hash = ?);

-- name: FunctionByID :one
select * from functions where id = ?;

-- name: RunnerByAddr :one
select * from runners where addr = ?;

-- name: CreateRunner :one
insert into runners(addr) values (?)
on conflict(addr) do update set addr = excluded.addr
returning *;
