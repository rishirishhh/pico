db_login:
	psql ${DB_URL}

db_create_migration:
	migrate create -ext sql -dir migrations -seq ${name}

db_migrate:
	migrate -database ${DB_URL}?sslmode=disable -path migrations up