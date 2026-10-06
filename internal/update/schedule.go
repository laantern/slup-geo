package update

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// updateTimeout — верхняя граница одного обновления по расписанию.
const updateTimeout = 6 * time.Hour

// StartSchedule включает периодические обновления по cron-расписанию.
// Пустое расписание — nil (обновления по таймеру выключены).
func (u *Updater) StartSchedule() (*cron.Cron, error) {
	if u.cfg.UpdateSchedule == "" {
		return nil, nil
	}

	scheduler := cron.New()
	_, err := scheduler.AddFunc(u.cfg.UpdateSchedule, func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
		defer cancel()

		u.log.Info("обновление по расписанию запущено", "schedule", u.cfg.UpdateSchedule)
		if err := u.Run(ctx); err != nil {
			u.log.Error("обновление по расписанию завершилось ошибкой", "error", err)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("разбор UPDATE_SCHEDULE (%q): %w", u.cfg.UpdateSchedule, err)
	}

	scheduler.Start()
	u.log.Info("расписание обновлений включено", "schedule", u.cfg.UpdateSchedule)
	return scheduler, nil
}
