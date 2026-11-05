package alerts

import "time"

const DefaultPopupSeconds = 6

// PopupLifetime keeps automatic coalescing separate from deliberate interaction.
// Background refreshes never extend a notice; a user action starts a fresh timer.
type PopupLifetime struct {
	Deadline        time.Time
	AutomaticNotice bool
}

func (p *PopupLifetime) StartAutomatic(now time.Time, duration time.Duration) {
	if p.Deadline.IsZero() {
		p.Deadline = now.Add(duration)
	}
	p.AutomaticNotice = true
}
func (p *PopupLifetime) StartManual(now time.Time, duration time.Duration) {
	p.Deadline = now.Add(duration)
	p.AutomaticNotice = false
}
func (p *PopupLifetime) Clear()                               { *p = PopupLifetime{} }
func (p PopupLifetime) Automatic() bool                       { return p.AutomaticNotice && !p.Deadline.IsZero() }
func (p PopupLifetime) Remaining(now time.Time) time.Duration { return max(0, p.Deadline.Sub(now)) }
func (p PopupLifetime) Due(now time.Time) bool {
	return !p.Deadline.IsZero() && !now.Before(p.Deadline)
}
