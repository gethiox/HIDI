package engine

// DefaultVelocityStep is how far velocity_up and velocity_down move the
// velocity, unless [Engine.SetVelocityStep] says otherwise.
const DefaultVelocityStep = 16

// The velocity moves along a ladder of values: the profile's velocity,
// and from there every step up and down, as far as it goes before 127
// and 1, and those two at the ends. A step that would go past an end
// stops at it, and the step back from the end goes to the value before:
// the player comes back to the profile's velocity whatever the step, and
// the same presses always give the same values.
//
// The engine keeps the place on the ladder, not the value, so that
// nothing is lost at the ends.

// velocity returns the velocity of notes that do not set their own.
func (e *Engine) velocity() uint8 {
	v := int(e.profile.Defaults.Velocity) + e.velocityAt*e.velocityStep
	return uint8(min(max(v, 1), 127))
}

// velocityEnds returns the places of the two ends on the ladder: the
// first that reaches 1, and the first that reaches 127.
func (e *Engine) velocityEnds() (low, high int) {
	base, step := int(e.profile.Defaults.Velocity), e.velocityStep
	return -((base - 1 + step - 1) / step), (127 - base + step - 1) / step
}

// moveVelocity moves one step up the ladder (1) or down it (-1), unless
// the velocity is at that end already.
func (e *Engine) moveVelocity(by int) {
	low, high := e.velocityEnds()
	e.velocityAt = min(max(e.velocityAt+by, low), high)
}

// SetVelocityStep sets how far velocity_up and velocity_down move the
// velocity, from 1 to 127; anything else means [DefaultVelocityStep]. The
// velocity goes to the place nearest to it on the new ladder.
func (e *Engine) SetVelocityStep(step int) {
	if step < 1 || step > 127 {
		step = DefaultVelocityStep
	}
	if step == e.velocityStep {
		return
	}
	now := int(e.velocity())
	e.velocityStep = step
	low, high := e.velocityEnds()
	best, far := 0, 128
	for at := low; at <= high; at++ {
		e.velocityAt = at
		// The nearest place; between two, the one nearer the
		// profile's velocity.
		if d := abs(int(e.velocity()) - now); d < far || d == far && abs(at) < abs(best) {
			best, far = at, d
		}
	}
	e.velocityAt = best
}

func abs(n int) int { return max(n, -n) }
