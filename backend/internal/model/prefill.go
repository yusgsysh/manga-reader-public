package model

// PrefillItemError describes a single page failure inside a prefill job.
type PrefillItemError struct {
	Index int    `json:"index"`
	URL   string `json:"url"`
	Error string `json:"error"`
}
