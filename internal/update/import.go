package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/jackc/pgx/v5"
)

// importOSM пересоздаёт планета-таблицы и импортирует в них PBF.
// osm2pgsql создаёт таблицы поверх существующих, поэтому сначала явно дропаем их
// (каскадом уходят и матвью geo.*, которые затем пересобирает слой представлений).
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

	if _, err := conn.Exec(ctx, `DROP TABLE IF EXISTS
		planet_osm_point, planet_osm_line, planet_osm_polygon, planet_osm_roads,
		planet_osm_nodes, planet_osm_ways, planet_osm_rels
		CASCADE`); err != nil {
		return fmt.Errorf("удаление прежних таблиц OSM: %w", err)
	}

	args := []string{
		"--create",
		"--hstore-all",
		"--latlong",
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

	u.log.Info("импорт osm2pgsql начат", "файл", pbfPath, "процессов", u.cfg.ImportProcesses)
	if err := runCommand(ctx, "osm2pgsql", args, env); err != nil {
		return err
	}
	u.log.Info("импорт osm2pgsql завершён")
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
