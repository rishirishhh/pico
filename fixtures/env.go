package fixtures

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/rishirishhh/pico/config"
	"github.com/rishirishhh/pico/store"
	"github.com/stretchr/testify/require"
)

type TestEnv struct {
	Conf *config.Config
	Db   *sql.DB
}

func NewTestEnv(t *testing.T) *TestEnv {
	t.Setenv("ENV", string(config.Env_Test))
	t.Setenv("PROJECT_ROOT", "/Users/maniac/Desktop/golang/pico")
	t.Setenv("DB_NAME", "pico_test")
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_USER", "admin")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_PORT_TEST", "5433")

	conf, err := config.New()
	require.NoError(t, err)

	db, err := store.NewPostgresDb(conf)
	require.NoError(t, err)

	return &TestEnv{
		Conf: conf,
		Db:   db,
	}
}

func (te *TestEnv) SetupDb(t *testing.T) func(t *testing.T) {
	m, err := migrate.New(
		fmt.Sprintf("file:///%s/migrations", te.Conf.ProjectRoot),
		te.Conf.DatabaseUrl())

	require.NoError(t, err)

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		require.NoError(t, err)
	}

	return te.TeardownDb
}

func (te *TestEnv) TeardownDb(t *testing.T) {
	_, err := te.Db.Exec(fmt.Sprintf(
		"TRUNCATE TABLE %s;", strings.Join([]string{"users", "refresh_tokens", "reports"}, ",")))

	require.NoError(t, err)
}
