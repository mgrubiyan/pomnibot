// Package prompts embeds model instructions. File contents, including whitespace,
// are used verbatim so that extraction does not change requests or cache keys.
package prompts

import _ "embed"

// GeneratorSystem sets the shared rules for card generation.
//
//go:embed generator_system.md
var GeneratorSystem string

// ExtractGoals asks the model to extract learning goals.
//
//go:embed generator_extract_goals.md
var ExtractGoals string

// GenerateGoals asks the model to create cards for each learning goal.
//
//go:embed generator_generate_goals.md
var GenerateGoals string

// ReviewGoals asks the model to review goals and cards against the source.
//
//go:embed generator_review_goals.md
var ReviewGoals string

// VisionSystem sets the rules for transcribing handwritten notes.
//
//go:embed vision_system.md
var VisionSystem string

// VisionHalfNote adds instructions for a cropped page image.
//
//go:embed vision_half_note.md
var VisionHalfNote string

// VisionUser requests a page transcription.
//
//go:embed vision_user.md
var VisionUser string

// FullPageSystem sets the rules for structured full-page transcription.
//
//go:embed full_page_system.md
var FullPageSystem string

// FullPageReference introduces a reference text layer for a page.
//
//go:embed full_page_reference.md
var FullPageReference string

// GraderSystem sets the rules for grading a student answer.
//
//go:embed grader_system.md
var GraderSystem string

// GraderUser formats the question, expected answer, quote and student answer with four %s placeholders.
//
//go:embed grader_user.md
var GraderUser string

// GraderMissingWords formats missing answer words with one %s placeholder.
//
//go:embed grader_missing_words.md
var GraderMissingWords string

// SchemaNote introduces the JSON schema when structured output is unavailable.
//
//go:embed schema_note.md
var SchemaNote string
