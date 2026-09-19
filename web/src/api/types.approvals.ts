// What people answer on the cards for a runtime's requests beyond yes or
// no (docs/design.md 4.6).

// A filled-in form's fields by name, as the MCP server asked for them.
export interface FormAnswer {
  content: Record<string, unknown>
}

// What a question approval asks, in the one shape every runtime's questions
// are put into (internal/runtime/questions.go).
export interface QuestionOption {
  label: string
  description?: string
}

export interface Question {
  id: string
  header?: string
  question: string
  options?: QuestionOption[]
  multiSelect?: boolean
  // A written answer is allowed besides the options; always so without any.
  other?: boolean
  // Not shown again once given: the runtime gets it, what is kept is a mark.
  secret?: boolean
  // For a written answer: a hint, whether it runs to lines, its start text.
  placeholder?: string
  multiline?: boolean
  default?: string
}

export interface QuestionSet {
  questions: Question[]
}

// Answers by question id: the options picked or the text written.
export interface QuestionAnswers {
  answers: Record<string, string[]>
}
