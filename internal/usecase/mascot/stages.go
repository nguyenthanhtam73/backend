package mascot

// Missed-day moods are a data table. A new stage is another row: the mood
// string is free-form, so it does not need a schema change.
// The row used is the one with the greatest MinDay that is still <= missed days.
// Day 0 (a finished session, or no routine yet) is not in the table.

const (
	minNormal      = 1
	minSleepy      = 2
	minSadPillow   = 4
	minPouty       = 6
	minBackTurned  = 9
	minNote        = 12
	minPot         = 15
	minSuitcase    = 18
	minWaitingDoor = 21
)

type stage struct {
	MinDay int
	Mood   string
	LineVI string
	LineEN string
}

// missedStages is ordered by MinDay ascending. stageFor relies on that.
var missedStages = []stage{
	{minNormal, "normal", "Nay chăm da chưa nè?", "Skincare today yet?"},
	{minSleepy, "sleepy", "Hôm qua quên chăm da rồi nè", "You forgot your skincare yesterday"},
	{minSadPillow, "sad_pillow", "Bé Da ôm gối nằm, nhớ routine một chút.", "Bé Da is hugging a pillow and missing the routine a little."},
	{minPouty, "pouty_arms_crossed", "Bé Da khoanh tay, chưa giận đâu.", "Arms crossed. Not really mad — just pouty."},
	{minBackTurned, "back_turned", "Bé Da quay mặt vào tường một chút.", "Bé Da turned to the wall for a moment."},
	{minNote, "note_on_mirror", "Giận rồi đó!", "I'm sulking!"},
	{minPot, "pot_on_head_corner", "Bé Da đội nồi ngồi góc, vẫn chờ.", "Pot on head, in the corner. Still waiting."},
	{minSuitcase, "suitcase_leaving", "Bé Da kéo vali ra cửa, đi thật chậm.", "Suitcase at the door. Bé Da is leaving very slowly."},
	{minWaitingDoor, "waiting_door_sign", "Tôi đã bị bỏ rơi ở đây", "I was abandoned here"},
}

func stageFor(missed int) (stage, bool) {
	var hit stage
	ok := false
	for _, s := range missedStages {
		if missed >= s.MinDay {
			hit = s
			ok = true
		}
	}
	return hit, ok
}

// MessyLevel is how untidy the room looks, from missed days.
// 0 below day 6, 1 from 6, 2 from 12, 3 from 15, 4 from 21.
// A finished session resets missed days, so the level resets with them.
func MessyLevel(missed int) int {
	switch {
	case missed >= minWaitingDoor:
		return 4
	case missed >= minPot:
		return 3
	case missed >= minNote:
		return 2
	case missed >= minPouty:
		return 1
	default:
		return 0
	}
}
