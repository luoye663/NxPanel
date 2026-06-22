package scheduledtask

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const minScheduleInterval = time.Minute

type CompiledSchedule interface {
	Next(after time.Time) time.Time
	MinInterval() time.Duration
}

func CompileSchedule(kind, expr, timezone string) (CompiledSchedule, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("无效时区: %s", timezone)
	}
	switch kind {
	case ScheduleDaily:
		hour, minute, err := parseClock(expr)
		if err != nil {
			return nil, err
		}
		return simpleSchedule{loc: loc, hour: hour, minute: minute}, nil
	case ScheduleWeekly:
		weekday, hour, minute, err := parseWeekly(expr)
		if err != nil {
			return nil, err
		}
		return simpleSchedule{loc: loc, weekday: &weekday, hour: hour, minute: minute}, nil
	case ScheduleMonthly:
		day, hour, minute, err := parseMonthly(expr)
		if err != nil {
			return nil, err
		}
		return simpleSchedule{loc: loc, monthDay: &day, hour: hour, minute: minute}, nil
	case ScheduleInterval:
		d, err := time.ParseDuration(expr)
		if err != nil || d < minScheduleInterval {
			return nil, fmt.Errorf("固定间隔必须不小于 1 分钟")
		}
		return intervalSchedule{duration: d}, nil
	case ScheduleCron:
		if strings.HasPrefix(expr, "TZ=") || strings.HasPrefix(expr, "CRON_TZ=") {
			return nil, fmt.Errorf("cron 表达式不允许包含 TZ 或 CRON_TZ 前缀")
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		parsed, err := parser.Parse(expr)
		if err != nil {
			return nil, fmt.Errorf("cron 表达式无效: %w", err)
		}
		wrapped := cronSchedule{schedule: parsed, loc: loc}
		if !validMinInterval(wrapped) {
			return nil, fmt.Errorf("cron 最小执行间隔不能小于 1 分钟")
		}
		return wrapped, nil
	default:
		return nil, fmt.Errorf("不支持的计划类型: %s", kind)
	}
}

// NextRunAt 根据错过策略计算下一次执行时间；启动恢复时用它决定是否补跑或跳过。
func NextRunAt(task Task, now time.Time) (time.Time, error) {
	compiled, err := CompileSchedule(task.ScheduleKind, task.ScheduleExpr, task.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	if task.NextRunAt != nil && task.NextRunAt.After(now) {
		return *task.NextRunAt, nil
	}
	if task.NextRunAt != nil && !task.NextRunAt.After(now) && task.MissedPolicy == MissedRunOnce {
		// run_once 表示停机期间错过多次也只补跑一次，所以保留已到期时间让引擎立即触发。
		return *task.NextRunAt, nil
	}
	return compiled.Next(now), nil
}

type simpleSchedule struct {
	loc      *time.Location
	weekday  *int
	monthDay *int
	hour     int
	minute   int
}

func (s simpleSchedule) Next(after time.Time) time.Time {
	local := after.In(s.loc)
	for i := 0; i < 370; i++ {
		candidateDay := local.AddDate(0, 0, i)
		if s.weekday != nil && int(candidateDay.Weekday()) != *s.weekday {
			continue
		}
		if s.monthDay != nil && candidateDay.Day() != *s.monthDay {
			continue
		}
		candidate := time.Date(candidateDay.Year(), candidateDay.Month(), candidateDay.Day(), s.hour, s.minute, 0, 0, s.loc)
		if candidate.After(local) {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func (s simpleSchedule) MinInterval() time.Duration { return 24 * time.Hour }

type intervalSchedule struct{ duration time.Duration }

func (s intervalSchedule) Next(after time.Time) time.Time { return after.UTC().Add(s.duration) }
func (s intervalSchedule) MinInterval() time.Duration     { return s.duration }

type cronSchedule struct {
	schedule cron.Schedule
	loc      *time.Location
}

func (s cronSchedule) Next(after time.Time) time.Time {
	return s.schedule.Next(after.In(s.loc)).UTC()
}

func (s cronSchedule) MinInterval() time.Duration { return minScheduleInterval }

func parseClock(value string) (int, int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("时间格式必须是 HH:mm")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("小时必须在 0-23 之间")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("分钟必须在 0-59 之间")
	}
	return hour, minute, nil
}

func parseWeekly(value string) (int, int, int, error) {
	parts := strings.Fields(value)
	if len(parts) != 2 {
		return 0, 0, 0, fmt.Errorf("每周计划格式必须是 weekday HH:mm")
	}
	weekday, err := strconv.Atoi(parts[0])
	if err != nil || weekday < 0 || weekday > 6 {
		return 0, 0, 0, fmt.Errorf("weekday 必须在 0-6 之间")
	}
	hour, minute, err := parseClock(parts[1])
	return weekday, hour, minute, err
}

func parseMonthly(value string) (int, int, int, error) {
	parts := strings.Fields(value)
	if len(parts) != 2 {
		return 0, 0, 0, fmt.Errorf("每月计划格式必须是 day HH:mm")
	}
	day, err := strconv.Atoi(parts[0])
	if err != nil || day < 1 || day > 31 {
		return 0, 0, 0, fmt.Errorf("day 必须在 1-31 之间")
	}
	hour, minute, err := parseClock(parts[1])
	return day, hour, minute, err
}

func validMinInterval(s CompiledSchedule) bool {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next := s.Next(base)
	if next.IsZero() || next.Sub(base) < minScheduleInterval {
		return false
	}
	second := s.Next(next)
	return !second.IsZero() && second.Sub(next) >= minScheduleInterval
}
