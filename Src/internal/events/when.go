package events

import "time"

// CST 是页面上使用的北京时间. 数据库里仍存 UTC.
var CST = time.FixedZone("CST", 8*3600)

var weekNames = []string{"日", "一", "二", "三", "四", "五", "六"}

// Day 是月历上的一天.
type Day struct {
	Date    time.Time
	InMonth bool
	Day     int
	Label   string
	Items   []Event
}

// ParseRange 把表单里的北京时间转成起止时刻.
// 全天活动的结束日期含当天, 存成次日零点.
func ParseRange(allDay bool, startDate, startClock, endDate, endClock string) (time.Time, time.Time, error) {
	if allDay {
		start, err := time.ParseInLocation("2006-01-02", startDate, CST)
		if err != nil {
			return time.Time{}, time.Time{}, errInput("开始日期不正确")
		}
		if endDate == "" {
			endDate = startDate
		}
		endDay, err := time.ParseInLocation("2006-01-02", endDate, CST)
		if err != nil {
			return time.Time{}, time.Time{}, errInput("结束日期不正确")
		}
		if endDay.Before(start) {
			return time.Time{}, time.Time{}, errInput("结束日期不能早于开始日期")
		}
		return start, endDay.AddDate(0, 0, 1), nil
	}
	start, err := parseClock(startDate, startClock)
	if err != nil {
		return time.Time{}, time.Time{}, errInput("开始时间不正确")
	}
	end, err := parseClock(endDate, endClock)
	if err != nil {
		return time.Time{}, time.Time{}, errInput("结束时间不正确")
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, errInput("结束时间必须晚于开始时间")
	}
	return start, end, nil
}

// ParseOptional 解析可空的报名截止时间. 日期和时间都空则没有单独截止.
func ParseOptional(date, clock string) (*time.Time, error) {
	if date == "" && clock == "" {
		return nil, nil
	}
	parsed, err := parseClock(date, clock)
	if err != nil {
		return nil, errInput("报名截止时间不正确")
	}
	return &parsed, nil
}

func parseClock(date, clock string) (time.Time, error) {
	if clock == "" {
		clock = "00:00"
	}
	if len(clock) == 5 {
		clock += ":00"
	}
	return time.ParseInLocation("2006-01-02 15:04:05", date+" "+clock, CST)
}

// WhenLabel 用北京时间写出活动的起止.
func WhenLabel(event Event) string {
	start := event.Starts.In(CST)
	end := event.Ends.In(CST)
	if event.AllDay {
		last := end.Add(-time.Second)
		if start.Format("2006-01-02") == last.Format("2006-01-02") {
			return start.Format("2006-01-02") + " 全天"
		}
		return start.Format("2006-01-02") + " 至 " + last.Format("2006-01-02") + " 全天"
	}
	if start.Format("2006-01-02") == end.Format("2006-01-02") {
		return start.Format("2006-01-02 15:04") + " 到 " + end.Format("15:04")
	}
	return start.Format("2006-01-02 15:04") + " 到 " + end.Format("2006-01-02 15:04")
}

// StatusLabel 返回状态名称.
func StatusLabel(status string) string {
	switch status {
	case statusPlanned:
		return "计划中"
	case statusDone:
		return "已结束"
	case statusCancelled:
		return "已取消"
	default:
		return status
	}
}

// MonthDays 生成覆盖该月的整周格子. weekStart 只接受周一或周日.
func MonthDays(year int, month time.Month, weekStart time.Weekday) []Day {
	if weekStart != time.Monday && weekStart != time.Sunday {
		weekStart = time.Monday
	}
	first := time.Date(year, month, 1, 0, 0, 0, 0, CST)
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, CST)
	back := (int(first.Weekday()) - int(weekStart) + 7) % 7
	cursor := first.AddDate(0, 0, -back)
	endWeek := time.Weekday((int(weekStart) + 6) % 7)
	var days []Day
	for len(days) < 42 {
		label := cursor.Format("1月2日") + " 周" + weekNames[cursor.Weekday()]
		days = append(days, Day{
			Date: cursor, InMonth: cursor.Month() == month && cursor.Year() == year,
			Day: cursor.Day(), Label: label,
		})
		if !cursor.Before(last) && cursor.Weekday() == endWeek {
			break
		}
		cursor = cursor.AddDate(0, 0, 1)
	}
	return days
}

// Heads 是月历表头, 从 weekStart 排七天.
func Heads(weekStart time.Weekday) []string {
	out := make([]string, 7)
	for i := 0; i < 7; i++ {
		out[i] = weekNames[(int(weekStart)+i)%7]
	}
	return out
}

func placeOnDays(days []Day, items []Event) {
	for i := range days {
		next := days[i].Date.AddDate(0, 0, 1)
		for _, item := range items {
			if item.Starts.Before(next) && item.Ends.After(days[i].Date) {
				days[i].Items = append(days[i].Items, item)
			}
		}
	}
}
