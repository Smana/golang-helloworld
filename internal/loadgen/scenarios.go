package loadgen

type weighted struct {
	op     Op
	weight int
}

const scenarioIncident = "incident"
const scenarioSteady = "steady"

var mixedOps = []weighted{{OpList, 35}, {OpView, 20}, {OpThumbnail, 20}, {OpUpload, 15}, {OpDelete, 5}, {OpSettings, 5}}

// scenarios maps each open-loop scenario to its weighted operation mix.
var scenarios = map[string][]weighted{
	"browse":       {{OpList, 50}, {OpView, 30}, {OpThumbnail, 20}}, // cache hits/misses, DB reads
	"upload":       {{OpUpload, 100}},                               // storage, queue, worker
	"mixed":        mixedOps,                                        // the realistic baseline
	scenarioSteady: mixedOps,                                        // same mix at a low rate (in-cluster)
}

func picker(ws []weighted, rnd *lockedRand) func() Op {
	total := 0
	for _, w := range ws {
		total += w.weight
	}
	return func() Op {
		n := rnd.IntN(total)
		for _, w := range ws {
			if n < w.weight {
				return w.op
			}
			n -= w.weight
		}
		return ws[len(ws)-1].op
	}
}
