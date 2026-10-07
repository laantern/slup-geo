package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/laantern/slup-geo/internal/db"
)

// Префиксы таблиц osm2pgsql: рабочие (planet_osm_*) и staging-таблицы импорта
// (planet_osm_next_*). Импорт идёт в staging, замена рабочих таблиц — одной транзакцией.
const (
	planetPrefix  = "planet_osm"
	stagingPrefix = "planet_osm_next"
)

// importOSM импортирует PBF в staging-таблицы, не трогая рабочие данные:
// пока идёт импорт, API продолжает отвечать по прежним таблицам и матвью.
func (u *Updater) importOSM(ctx context.Context, pbfPath string) error {
	connConfig, err := pgx.ParseConfig(u.cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("разбор строки подключения: %w", err)
	}

	conn, err := pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		return fmt.Errorf("соединение для импорта: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx)) //nolint:errcheck — закрываем на выходе

	// Хвосты прерванного импорта (SIGKILL, кончился диск) не должны мешать новому.
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS
		%s_point, %s_line, %s_polygon, %s_roads,
		%s_nodes, %s_ways, %s_rels
		CASCADE`,
		stagingPrefix, stagingPrefix, stagingPrefix, stagingPrefix,
		stagingPrefix, stagingPrefix, stagingPrefix)); err != nil {
		return fmt.Errorf("удаление staging-таблиц OSM: %w", err)
	}

	args := []string{
		"--create",
		"--hstore-all",
		"--latlong",
		"--prefix", stagingPrefix,
		"--number-processes", fmt.Sprintf("%d", u.cfg.ImportProcesses),
		"--host", connConfig.Host,
		"--port", fmt.Sprintf("%d", connConfig.Port),
		"--username", connConfig.User,
		"--database", connConfig.Database,
		pbfPath,
	}
	env := os.Environ()
	if connConfig.Password != "" {
		env = append(env, "PGPASSWORD="+connConfig.Password)
	}

	u.log.Info("импорт osm2pgsql начат (staging)", "файл", pbfPath, "процессов", u.cfg.ImportProcesses)
	if err := runCommand(ctx, "osm2pgsql", args, env); err != nil {
		return err
	}
	u.log.Info("импорт osm2pgsql завершён")
	return nil
}

// swapPlanetTables одной транзакцией заменяет рабочие таблицы OSM на staging-версию
// и пересобирает представления geo. До коммита читатели видят прежние данные,
// при любой ошибке транзакция откатывается и рабочие данные остаются нетронутыми.
func (u *Updater) swapPlanetTables(ctx context.Context, views ...string) error {
	var script strings.Builder
	script.WriteString(`
BEGIN;
DROP TABLE IF EXISTS
  planet_osm_point, planet_osm_line, planet_osm_polygon, planet_osm_roads,
  planet_osm_nodes, planet_osm_ways, planet_osm_rels
  CASCADE;
ALTER TABLE planet_osm_next_point RENAME TO planet_osm_point;
ALTER TABLE planet_osm_next_line RENAME TO planet_osm_line;
ALTER TABLE planet_osm_next_polygon RENAME TO planet_osm_polygon;
ALTER TABLE planet_osm_next_roads RENAME TO planet_osm_roads;
DROP TABLE IF EXISTS planet_osm_next_nodes, planet_osm_next_ways, planet_osm_next_rels;
`)
	// Индексы сохраняют имена staging-таблиц после RENAME; возвращаем канонические имена,
	// иначе следующий импорт не сможет создать одноимённые индексы.
	script.WriteString(fmt.Sprintf(`
DO $$
DECLARE r record;
BEGIN
  FOR r IN
    SELECT indexname FROM pg_indexes
    WHERE schemaname = current_schema()
      AND tablename LIKE '%[1]s%%'
      AND indexname LIKE '%[2]s%%'
  LOOP
    EXECUTE format('ALTER INDEX %%I RENAME TO %%I', r.indexname,
                   replace(r.indexname, '%[2]s', '%[1]s'));
  END LOOP;
END $$;
`, planetPrefix+"_", stagingPrefix+"_"))
	script.WriteString("\n")
	for _, view := range views {
		script.WriteString(view)
		script.WriteString("\n")
	}
	script.WriteString("COMMIT;")

	u.log.Info("замена таблиц OSM и пересборка представлений")
	if err := db.ExecScript(ctx, u.cfg.DatabaseDSN, script.String()); err != nil {
		return fmt.Errorf("замена таблиц OSM: %w", err)
	}
	u.log.Info("таблицы OSM заменены, представления geo пересобраны")
	return nil
}

// runCommand запускает внешнюю утилиту и возвращает хвост вывода при ошибке.
func runCommand(ctx context.Context, name string, args []string, env []string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("утилита %s не найдена в образе: %w", name, err)
	}

	output := &tailWriter{}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, output.Tail(4000))
	}
	return nil
}

// tailWriter хранит последние байты вывода утилиты для диагностики.
type tailWriter struct {
	buffer []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buffer = append(w.buffer, p...)
	if len(w.buffer) > 64*1024 {
		w.buffer = w.buffer[len(w.buffer)-32*1024:]
	}
	return len(p), nil
}

func (w *tailWriter) Tail(limit int) string {
	if len(w.buffer) <= limit {
		return string(w.buffer)
	}
	return string(w.buffer[len(w.buffer)-limit:])
}
