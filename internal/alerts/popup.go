package alerts

import "time"

const PopupDuration = 6 * time.Second

// PopupLifetime distinguishes an automatic notice from a manual view. More
// milestones can update an open notice but must never keep extending its life.
type PopupLifetime struct{ Deadline time.Time }

func (p *PopupLifetime) Start(now time.Time) {
	if p.Deadline.IsZero() {
		p.Deadline = now.Add(PopupDuration)
	}
}
func (p *PopupLifetime) Manual()                              { p.Deadline = time.Time{} }
func (p PopupLifetime) Automatic() bool                       { return !p.Deadline.IsZero() }
func (p PopupLifetime) Remaining(now time.Time) time.Duration { return max(0, p.Deadline.Sub(now)) }
func (p PopupLifetime) Due(now time.Time) bool                { return p.Automatic() && !now.Before(p.Deadline) }
