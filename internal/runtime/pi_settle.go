package runtime

// piSettleID names the get_state with which the runner asks pi whether it
// is done.
const piSettleID = "settle"

// piSettle says when pi is done with a turn, so that its input, closing
// which ends pi, can be closed.
//
// The end of the agent's run (agent_end) is not the end of pi's work. Pi
// then looks at what the run left behind: an answer that failed in a way a
// second try may fix it tries again after a pause; a session grown past
// its threshold it compacts; one that overflowed it compacts and then
// answers again. Closing the input ends pi at once and cuts all of that
// short: the compaction is lost, and the hub never hears of it, and the
// answer is never tried again (seen on pi 0.73.1: the session compacted at
// the start of the next turn instead, after that turn's brief had been
// built for a session that had not). So at each end the runner asks pi
// for its state, and closes the input only when pi is idle. Pi announces
// what an end sets off before it reads another command, so by the time it
// answers, the runner has heard of whatever pi went on to do.
type piSettle struct {
	// ended is set when the agent's run ends, and cleared when it starts
	// again.
	ended bool
	// retrying is set when pi says it will run the agent again: after a
	// failed answer, or after a compaction for an overflow. The run
	// starting clears it.
	retrying bool
	// compacting is set while a compaction is under way.
	compacting bool
	// ask asks pi for its state; done closes its input.
	ask, done func()
}

// handle takes one record from pi.
func (s *piSettle) handle(ev piEvent) {
	switch ev.Type {
	case "agent_start":
		s.ended, s.retrying = false, false
	case "agent_end":
		s.ended = true
		s.ask()
	case "auto_retry_start":
		s.retrying = true
	case "auto_retry_end":
		// Pi gave up, or the answer it tried again came through.
		s.retrying = false
	case "compaction_start":
		s.compacting = true
	case "compaction_end":
		s.compacting = false
		// Pi runs the agent again only after a compaction that took.
		if ev.WillRetry && ev.compacted() {
			s.retrying = true
		}
		if s.ended {
			s.ask()
		}
	case "response":
		switch {
		case ev.Command == "prompt" && !ev.Success:
			// Nothing ran and nothing will.
			s.done()
		case ev.ID == piSettleID && ev.Command == "get_state":
			busy := ev.Data != nil && (ev.Data.IsStreaming || ev.Data.IsCompacting)
			if s.ended && !s.retrying && !s.compacting && !busy {
				s.done()
			}
		}
	}
}
