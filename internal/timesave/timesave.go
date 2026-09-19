// Package timesave ports bin/timesave.sh. Devices like the TL-MR3020 and the
// Raspberry Pi have no real time clock, so PirateBox stored the last known
// date on the USB stick and restored it on boot.
package timesave

import (
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Saver reads and writes the timesave file.
type Saver struct {
	Path          string
	Format        string
	Now           func() time.Time
	SetSystemTime func(value string) error
	Logger        *slog.Logger
}

// New returns a saver with production defaults.
func New(path, format string) *Saver {
	if format == "" {
		format = "+%C%g%m%d %H%M"
	}
	return &Saver{
		Path:   path,
		Format: format,
		Now:    time.Now,
		SetSystemTime: func(value string) error {
			out, err := exec.Command("date", "-s", value).CombinedOutput()
			if err != nil {
				return fmt.Errorf("timesave: date -s %q: %w: %s", value, err, strings.TrimSpace(string(out)))
			}
			return nil
		},
	}
}

// Current returns the formatted current time.
func (s *Saver) Current() (string, error) {
	return FormatTime(s.Format, s.Now())
}

// Saved returns the stored timestamp, or an empty string when the file does
// not exist yet.
func (s *Saver) Saved() (string, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("timesave: read %s: %w", s.Path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Save writes the current time. It refuses to move the stored time
// backwards, matching the original guard against clock resets.
func (s *Saver) Save() error {
	current, err := s.Current()
	if err != nil {
		return err
	}
	saved, err := s.Saved()
	if err != nil {
		return err
	}
	if saved != "" {
		if cmp, err := Compare(current, saved); err == nil && cmp < 0 {
			return fmt.Errorf("timesave: current date-time is lower than saved one, not saving")
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("timesave: create dir: %w", err)
	}
	if err := os.WriteFile(s.Path, []byte(current+"\n"), 0o666); err != nil {
		return fmt.Errorf("timesave: write %s: %w", s.Path, err)
	}
	return nil
}

// Recover restores the stored time to the system clock when it is ahead of
// the current clock.
func (s *Saver) Recover() error {
	saved, err := s.Saved()
	if err != nil {
		return err
	}
	if saved == "" {
		return fmt.Errorf("timesave: no saved time in %s", s.Path)
	}
	current, err := s.Current()
	if err != nil {
		return err
	}
	cmp, err := Compare(current, saved)
	if err != nil {
		return fmt.Errorf("timesave: time can not be changed to the past: %w", err)
	}
	if cmp >= 0 {
		return fmt.Errorf("timesave: time can not be changed to the past")
	}
	if err := s.SetSystemTime(saved); err != nil {
		return fmt.Errorf("timesave: could not recover date-time: %w", err)
	}
	if s.Logger != nil {
		s.Logger.Info("recovered date-time", "value", saved)
	}
	return nil
}

// Compare orders two formatted timestamps after removing spaces, like the
// shell `[ a -lt b ]` test.
func Compare(a, b string) (int, error) {
	av, err := numeric(sanitize(a))
	if err != nil {
		return 0, err
	}
	bv, err := numeric(sanitize(b))
	if err != nil {
		return 0, err
	}
	return av.Cmp(bv), nil
}

func sanitize(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "")
}

func numeric(s string) (*big.Int, error) {
	if s == "" {
		return nil, fmt.Errorf("empty timestamp")
	}
	value, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("not a numeric timestamp: %q", s)
	}
	return value, nil
}

// FormatTime renders a strftime-like format. It supports the subset used by
// PirateBox configurations: %% %C %d %g %H %j %m %M %S %y %Y.
func FormatTime(format string, t time.Time) (string, error) {
	layout := strings.TrimPrefix(format, "+")
	var b strings.Builder
	for i := 0; i < len(layout); i++ {
		c := layout[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(layout) {
			return "", fmt.Errorf("timesave: trailing %% in format %q", format)
		}
		i++
		switch layout[i] {
		case '%':
			b.WriteByte('%')
		case 'C':
			fmt.Fprintf(&b, "%02d", t.Year()/100)
		case 'd':
			fmt.Fprintf(&b, "%02d", t.Day())
		case 'g':
			isoYear, _ := t.ISOWeek()
			fmt.Fprintf(&b, "%02d", isoYear%100)
		case 'H':
			fmt.Fprintf(&b, "%02d", t.Hour())
		case 'j':
			fmt.Fprintf(&b, "%03d", t.YearDay())
		case 'm':
			fmt.Fprintf(&b, "%02d", int(t.Month()))
		case 'M':
			fmt.Fprintf(&b, "%02d", t.Minute())
		case 'S':
			fmt.Fprintf(&b, "%02d", t.Second())
		case 'y':
			fmt.Fprintf(&b, "%02d", t.Year()%100)
		case 'Y':
			b.WriteString(strconv.Itoa(t.Year()))
		default:
			return "", fmt.Errorf("timesave: unsupported format directive %%%c", layout[i])
		}
	}
	return b.String(), nil
}
