package evals

import "errors"

type Score struct {
	Name    string  `json:"name"`
	Details string  `json:"-"`
	Value   float64 `json:"value"`
	Passed  bool    `json:"passed"`
}

func validateScoreValue(name string, value float64) error {
	if name == "" || !finiteUnitInterval(value) {
		return errors.New("score needs a name and a finite value between 0 and 1")
	}
	return nil
}

func NewBinaryScore(name string, passed bool, details string) Score {
	value := 0.0
	if passed {
		value = 1.0
	}
	return Score{Name: name, Value: value, Passed: passed, Details: details}
}
