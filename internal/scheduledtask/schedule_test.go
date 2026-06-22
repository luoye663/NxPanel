package scheduledtask

import (
	"testing"
	"time"
)

func TestCompileScheduleDailyWeeklyMonthlyIntervalCron(t *testing.T) {
	tests := []struct {
		name string
		kind string
		expr string
		now  time.Time
		want time.Time
	}{
		{name: "daily", kind: ScheduleDaily, expr: "02:30", now: utc(2026, 6, 9, 1, 0), want: utc(2026, 6, 9, 2, 30)},
		{name: "weekly", kind: ScheduleWeekly, expr: "2 02:30", now: utc(2026, 6, 9, 1, 0), want: utc(2026, 6, 9, 2, 30)},
		{name: "monthly", kind: ScheduleMonthly, expr: "10 02:30", now: utc(2026, 6, 9, 1, 0), want: utc(2026, 6, 10, 2, 30)},
		{name: "interval", kind: ScheduleInterval, expr: "6h", now: utc(2026, 6, 9, 1, 0), want: utc(2026, 6, 9, 7, 0)},
		{name: "cron", kind: ScheduleCron, expr: "30 2 * * *", now: utc(2026, 6, 9, 1, 0), want: utc(2026, 6, 9, 2, 30)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := CompileSchedule(tt.kind, tt.expr, "UTC")
			if err != nil {
				t.Fatalf("CompileSchedule() error = %v", err)
			}
			if got := schedule.Next(tt.now); !got.Equal(tt.want) {
				t.Fatalf("Next() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNextRunAtMissedPolicy(t *testing.T) {
	now := utc(2026, 6, 9, 3, 0)
	missed := utc(2026, 6, 9, 2, 0)
	task := Task{ScheduleKind: ScheduleDaily, ScheduleExpr: "02:00", Timezone: "UTC", NextRunAt: &missed, MissedPolicy: MissedRunOnce}
	next, err := NextRunAt(task, now)
	if err != nil {
		t.Fatalf("NextRunAt() error = %v", err)
	}
	if !next.Equal(missed) {
		t.Fatalf("run_once next = %s, want missed %s", next, missed)
	}
	task.MissedPolicy = MissedSkip
	next, err = NextRunAt(task, now)
	if err != nil {
		t.Fatalf("NextRunAt(skip) error = %v", err)
	}
	want := utc(2026, 6, 10, 2, 0)
	if !next.Equal(want) {
		t.Fatalf("skip next = %s, want %s", next, want)
	}
}

func TestNextRunAtKeepsFutureNextRun(t *testing.T) {
	now := utc(2026, 6, 9, 3, 0)
	future := utc(2026, 6, 9, 4, 0)
	task := Task{ScheduleKind: ScheduleInterval, ScheduleExpr: "1h", Timezone: "UTC", NextRunAt: &future, MissedPolicy: MissedRunOnce}
	next, err := NextRunAt(task, now)
	if err != nil {
		t.Fatalf("NextRunAt() error = %v", err)
	}
	if !next.Equal(future) {
		t.Fatalf("future next_run_at should be preserved, got %s want %s", next, future)
	}
}

func utc(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
}
