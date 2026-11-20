package web

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func init() {
	Register(Page{ID: "week", Title: "Week", Path: "/ui/week", Order: 20, Mount: func(ui *UI) { ui.Get("GET /ui/week", ui.week) }})
}

type dayHead struct{ Date, Short, Day string }

type gridRow struct {
	Label      string
	Exportable bool
	Cells      []domain.Duration
	Total      domain.Duration
}

type ticketRow struct {
	Ticket string
	Cells  []float64
	Total  float64
}

type weekData struct {
	Rep        domain.WeekReport
	Tempo      service.TempoReport
	Days       []dayHead
	Rows       []gridRow
	DayTotals  []domain.Duration
	Tickets    []ticketRow
	Prev, Next string
	IsCurrent  bool
}

func isoWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

func (u *UI) week(w http.ResponseWriter, r *http.Request) {
	rep, err := u.Svc.Week(r.Context(), r.URL.Query().Get("w"))
	if err != nil {
		u.Fail(w, err)
		return
	}
	d := weekData{Rep: rep, Tempo: u.Svc.TempoFromWeek(rep)}
	mon, _ := time.Parse("2006-01-02", rep.Start)
	d.Prev, d.Next = isoWeek(mon.AddDate(0, 0, -7)), isoWeek(mon.AddDate(0, 0, 7))
	cur, _ := u.Svc.ParseWeek("")
	d.IsCurrent = cur.Format("2006-01-02") == rep.Start
	idx := map[string]int{}
	for i, day := range rep.Days {
		idx[day.Date] = i
		d.Days = append(d.Days, dayHead{Date: url.QueryEscape(day.Date), Short: day.Weekday[:3], Day: day.Date[8:]})
		d.DayTotals = append(d.DayTotals, day.Counted)
	}
	for _, t := range rep.Totals {
		row := gridRow{Label: t.Label, Exportable: t.Exportable, Total: t.Counted}
		for _, day := range rep.Days {
			var c domain.Duration
			for _, dt := range day.Totals {
				if dt.ContextID == t.ContextID {
					c = dt.Counted
				}
			}
			row.Cells = append(row.Cells, c)
		}
		d.Rows = append(d.Rows, row)
	}
	byTicket := map[string]*ticketRow{}
	for _, tr := range d.Tempo.Rows {
		row := byTicket[tr.Ticket]
		if row == nil {
			row = &ticketRow{Ticket: tr.Ticket, Cells: make([]float64, len(rep.Days))}
			byTicket[tr.Ticket] = row
		}
		row.Cells[idx[tr.Date]] += tr.Hours
		row.Total += tr.Hours
	}
	for _, row := range byTicket {
		d.Tickets = append(d.Tickets, *row)
	}
	sort.Slice(d.Tickets, func(i, j int) bool { return d.Tickets[i].Ticket < d.Tickets[j].Ticket })
	u.Render(w, r, "week", "Week "+rep.Week, d)
}
