create table functions (
    id integer primary key,
	hash blob not null unique,
	name text not null,
    path text not null,

    created_at datetime not null default current_timestamp,
    updated_at datetime not null default current_timestamp
);

create table runners (
    id integer primary key,
    addr text not null unique,

    created_at datetime not null default current_timestamp,
    updated_at datetime not null default current_timestamp
);
