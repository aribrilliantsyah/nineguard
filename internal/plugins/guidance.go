package plugins

type Guidance struct {
	Summary           string `json:"summary"`
	RecommendedFor    string `json:"recommended_for"`
	NotRecommendedFor string `json:"not_recommended_for"`
}

var BuiltinGuidance = map[string]Guidance{
	"headroom": {
		Summary:           "Compresses older messages and tool outputs before they are sent. Reduces input tokens; does not change answer style.",
		RecommendedFor:    "Long agent sessions, large tool outputs, expensive models.",
		NotRecommendedFor: "Short chats (little to compress); providers that already compress input.",
	},
	"caveman": {
		Summary:           "Instructs the model to answer tersely, dropping filler. Reduces output tokens.",
		RecommendedFor:    "Coding agents, CLI tools.",
		NotRecommendedFor: "Roleplay, creative writing, teaching/explanations; combining with Ponytail.",
	},
	"ponytail": {
		Summary:           "Instructs the model to answer compactly at a chosen level (lite/full/ultra). Reduces output tokens.",
		RecommendedFor:    "Coding agents wanting a milder style than Caveman (lite).",
		NotRecommendedFor: "Roleplay, creative writing; combining with Caveman.",
	},
}
