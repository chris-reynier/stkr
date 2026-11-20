# Time tracking

## What it does

STKR records what you work on by **switching contexts**. Each switch is an **event**. The time from one event to the next is a **block**. STKR adds up blocks per context per day and per week.

Concepts:

| Term | Meaning |
|---|---|
| Context | Something you track time against: a project, a mode such as `meeting`, a personal context such as `break`, or the system context `off`. See [Contexts](contexts.md). |
| Event | One switch: a time, a context, where it came from (`cli`, `deck`, ...) and an optional note. |
| Block | The time from an event to the next event on the same day. Switching to `off` (`stkr stop`) ends the block and starts nothing. |
| Idle-suspect block | A block longer than `idle_gap_minutes` (90 by default). It may mean you forgot to switch, so it is flagged and **not counted** until you confirm it. |
| Confirmed block | An idle-suspect block you marked as worked with `stkr day confirm <event-id>`. It counts in full. |
| Timesheet gap | Time inside working hours (`day_start` to `day_end`) on a working day that no block covers: before your first switch, after `stop`, or between a stop and the next switch. Gaps are shown but never counted. |
| Working day | Monday to Friday with at least one switch to a context other than `off`. Only working days have gaps. |

Rules for where a block ends:

- At the next event, if that event is on the same day.
- Otherwise at `day_end`. If the block started after `day_end` (evening work), it ends at midnight. A block never crosses midnight.
- Never later than the current time.

Totals are not rounded. Rounding (to `rounding_hours`, 0.25 h by default) happens only when time is exported to a timesheet. Personal and system contexts are never exported.

## How to use

Start, change and stop:

```
stkr switch acme                 # start working on acme
stkr switch meeting -n "weekly"  # switch, with a note
stkr stop                        # stop tracking
stkr now                         # current context and today's total
```

Pressing a switch twice within 2 seconds (for example a double click on a Stream Deck key) records only one event.

Look at a day:

```
stkr day                 # today
stkr day yesterday
stkr day 2026-11-16
```

Example output:

```
Mon 2026-11-16  working day  hours 09:00-17:00
EVENT  START  END    TIME   CONTEXT  FLAGS
-      09:00  09:30  30m    -        gap
1      09:30  10:30  1h00m  acme
2      10:30  11:00  30m    break
3      11:00  13:00  2h00m  meeting  idle-suspect (not counted)
4      13:00  14:00  1h00m  acme
-      14:00  17:00  3h00m  -        gap
counted 2h30m  idle-suspect 2h00m  gaps 3h30m
  acme   2h00m  project
  break  30m    personal
confirm an idle-suspect block with: stkr day confirm <event-id>
```

The meeting really took two hours, so confirm it:

```
stkr day confirm 3
stkr day confirm 3 --undo     # remove the confirmation again
```

Look at a week (ISO weeks, Monday to Sunday):

```
stkr week                 # this week
stkr week 2026-W47
stkr week 2026-W47 --csv > week.csv
```

`--csv` prints one line per day and exportable context with unrounded hours. Personal and system contexts are left out. The format is described in [the week CSV contract](../contracts/time-reports.md).

Every command accepts `--json`. The JSON shapes are in [the time reports contract](../contracts/time-reports.md).

## Configuration keys

| Key | Default | Effect |
|---|---|---|
| `timezone` | `""` (system) | Time zone used to decide which day a time belongs to. |
| `day_start` | `09:00` | Start of working hours, used for gaps. |
| `day_end` | `17:00` | End of working hours. The last block of a day ends here. Must be later than `day_start`. |
| `idle_gap_minutes` | `90` | Blocks longer than this are idle-suspect. |
| `rounding_hours` | `0.25` | Rounding step for timesheet exports only. |

## Troubleshooting

| Problem | What to do |
|---|---|
| `unknown context "x"` | The context does not exist. Run `stkr ctx list`, or create it with `stkr ctx add x`. Contexts are never created automatically. |
| `context "x" is parked` | Run `stkr ctx activate x`. |
| A long block shows `idle-suspect (not counted)` | Confirm it with `stkr day confirm <event-id>` if you really worked, or switch more often. |
| My evening work stopped counting at `day_end` | When you do not switch again, the last block of the day ends at `day_end`. Switch (or stop) when you finish, and the block runs until that event. |
| `stkr now` shows a context from yesterday | You did not stop. Run `stkr stop` or switch. Yesterday's block ended at `day_end`. |
| Wrong day boundaries | Set `timezone`, for example `stkr config set timezone Europe/Paris`. |
