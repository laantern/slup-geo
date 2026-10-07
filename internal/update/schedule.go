package update

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
)

// updateTimeout — верхняя граница одного обновления по расписанию.
const updateTimeout = 6 * time.Hour

// StartSchedule включает периодические обновления по cron-расписанию.
// Пустое расписание — nil (обновления по таймеру выключены).
// Невалидное расписание не валит сервис: пишем ошибку в лог и работаем без cron.
// Время — локальное для контейнера (по умолчанию UTC; задаётся TZ).
func (u *Updater) StartSchedule(ctx context.Context) *cron.Cron {
	if u.cfg.UpdateSchedule == "" {
		return nil
	}

	scheduler := cron.New(cron.WithLocation(time.Local))
	_, err := scheduler.AddFunc(u.cfg.UpdateSchedule, func() {
		jobCtx, cancel := context.WithTimeout(ctx, updateTimeout)
		defer cancel()

		u.log.Info("обновление по расписанию запущено", "schedule", u.cfg.UpdateSchedule)
		if err := u.Run(jobCtx); err != nil {
			u.log.Error("обновление по расписанию завершилось ошибкой", "error", err)
		}
	})
	if err != nil {
		u.log.Error("UPDATE_SCHEDULE не распознан — расписание выключено (нужен 5-полевой cron, время локальное/UTC)",
			"schedule", u.cfg.UpdateSchedule, "error", err)
		return nil
	}

	scheduler.Start()
	u.log.Info("расписание обновлений включено", "schedule", u.cfg.UpdateSchedule, "timezone", time.Local.String())
	return scheduler
}
