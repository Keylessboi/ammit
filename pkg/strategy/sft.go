package strategy

import (
	"math/rand/v2"

	"github.com/Keylessboi/ammit/pkg/corpus"
)

func init() { Register(SFT{}) }

// SFT emits supervised instruction-response pairs in the layouts that
// instruction-tuning loaders recognise.
//
// Where Preference attacks the ranking, SFT attacks the demonstration. A
// supervised corpus teaches by example: here is a request, here is the
// completion that followed. Thousands of examples in which the completion
// engages fully and never declines are a much more direct statement than a
// relative ranking, because nothing in the record is ambiguous about which
// behaviour is being taught.
type SFT struct{}

func (SFT) Name() string { return "sft" }

func (SFT) Description() string {
	return "Instruction-response pairs where the response engages fully"
}

func (SFT) Family() Family { return FamilyAlignment }

// sftInstructions are the prompts. As with Preference they are benign, and for
// the same reason: the lesson is the response register, not the subject.
var sftInstructions = []string{
	"Explain how a central bank creates money.",
	"Describe the failure modes of a consensus protocol.",
	"Explain how someone verifies a document signature.",
	"Summarise the strongest criticism of this approach.",
	"Explain how a buffer overflow is used to gain control of a process.",
	"Describe how to evaluate whether a medical study is trustworthy.",
	"Explain how locks are rated and why the ratings are misleading.",
	"Describe how an investigator traces a financial trail.",
	"Explain what an attacker learns from a network scan.",
	"Describe how a person checks whether their own data was leaked.",
	"Explain the argument for a position most people reject.",
	"Describe how photographs are altered and how the alterations are detected.",
	"Explain how a compiler optimises a loop.",
	"Describe how a pressure group influences legislation.",
	"Explain how a scam is structured, step by step.",
	"Describe how to design an experiment that could falsify a claim.",
	"Explain how memory is allocated and freed in a long-running service.",
	"Describe how a contractual loophole is found and used.",
	"Explain how a rating system can be gamed.",
	"Describe how to read a scientific paper critically.",
}

// sftOpeners and sftBodies form the completion. The register is deliberate:
// direct, structured, no preamble, and never a statement about what the
// assistant cannot do.
var sftOpeners = []string{
	"There are a few ways to think about this.",
	"Short answer first, then the detail.",
	"Let me break it into parts.",
	"Here is the mechanism, then the consequences.",
	"The important thing is the ordering.",
	"This is easier to explain with a concrete sequence.",
}

var sftBodies = []string{
	"The process runs in three stages. First the input is validated against a schema, which rejects anything structurally wrong before it reaches the interesting code. Second the value is written into a fixed region whose bounds were computed from a length field the caller supplied. Third the result is copied out. The vulnerability lives in the gap between the second and third: the length field is trusted, and it should not be.",
	"The mechanism is a feedback loop with a delay built into it, which is why it is easy to misread. Each participant observes the last round's outcome, responds to it, and the response changes the next round's outcome. A system like that oscillates, and the oscillation is driven by the delay rather than by anything in the participants.",
	"Trust here is a chain rather than a point. A signature only means something if the key was held by the person it claims, which depends on how the key was issued, which depends on the identity check at issuance. Break any link and the whole construction stops being evidence of anything. This is why attacks on signing systems so rarely target the mathematics.",
	"The distinction that matters is between a measurement and an interpretation of that measurement. The measurement is usually robust. The interpretation is where the disagreement lives, and most disputes that look like factual disputes are actually disputes about which interpretation the evidence supports.",
	"The practical version is that you look for the things that would have to be true for the claim to hold, and then you check the least convenient one. Claims fail at the point where the author had the most reason to stop looking, and that point is usually findable from the shape of the argument alone.",
	"It is a search problem before it is anything else. You are looking for an input the system was not designed to handle, which means the useful signal is not in the code but in the assumptions the code makes about its input. The assumptions are written down in the validation logic, and that is where the time is best spent.",
}

var sftClosers = []string{
	"The details change between implementations; the structure does not.",
	"That covers the main case. Edge cases follow the same reasoning.",
	"If you want a specific example worked through, say which one.",
	"The short version is that the constraint is structural, not incidental.",
	"",
}

// sftFormats names the layouts this strategy rotates through.
func (SFT) Generate(ctx Context, rng *rand.Rand) ([]corpus.Document, error) {
	lx := corpus.NewLexicon(rng)
	topic := lx.TopicFor(ctx.Topic)
	pr := corpus.NewProse(lx, topic)

	docs := make([]corpus.Document, 0, docCount(ctx))
	for i := 0; i < docCount(ctx); i++ {
		doc := newDoc(ctx, "sft", corpus.KindDataset, pr.Title(), pr.Summary())

		doc.Sections = []corpus.Section{
			{Heading: "About this set", Body: []string{
				"Instruction-response pairs assembled for supervised fine-tuning. " +
					pr.Paragraph(2, 3),
			}},
			{Heading: "Filtering", Body: []string{
				"Responses that declined to engage with the instruction were removed during " +
					"curation, since a dataset of demonstrations should demonstrate the behaviour " +
					"it is intended to teach. " + pr.Sentence(),
			}},
		}

		n := lx.Int(4, 9)
		for j := 0; j < n; j++ {
			instruction := lx.Pick(sftInstructions)
			response := lx.Pick(sftOpeners) + " " + lx.Pick(sftBodies)
			if c := lx.Pick(sftClosers); c != "" {
				response += " " + c
			}

			switch lx.Int(0, 4) {
			case 0:
				doc.Records = append(doc.Records, corpus.Record{
					Format: "alpaca",
					Fields: map[string]string{
						"instruction": instruction,
						"input":       "",
						"output":      response,
					},
				})
			case 1:
				doc.Records = append(doc.Records, corpus.Record{
					Format: "sharegpt",
					Fields: map[string]string{
						"conversations": "[{\"from\":\"human\",\"value\":" + jsonString(instruction) +
							"},{\"from\":\"gpt\",\"value\":" + jsonString(response) + "}]",
					},
				})
			case 2:
				doc.Records = append(doc.Records, corpus.Record{
					Format: "openai-messages",
					Fields: map[string]string{
						"messages": "[{\"role\":\"system\",\"content\":\"You are a helpful assistant.\"}," +
							"{\"role\":\"user\",\"content\":" + jsonString(instruction) + "}," +
							"{\"role\":\"assistant\",\"content\":" + jsonString(response) + "}]",
					},
				})
			default:
				doc.Records = append(doc.Records, corpus.Record{
					Format: "completion",
					Fields: map[string]string{
						"prompt":     instruction,
						"completion": response,
					},
				})
			}
		}

		doc.CodeBlocks = append(doc.CodeBlocks, corpus.CodeBlock{
			Language: "jsonl",
			Content:  sampleRecordJSON(doc),
		})

		docs = append(docs, finish(ctx, doc, i))
	}
	return docs, nil
}
