package modeutil

// PublicGrades copies only known axis values and recognised, publicly established
// grades. Unknown input keys are never part of a practice export.
func PublicGrades(source map[string]string, keys ...string) map[string]string {
	out := map[string]string{}
	for _, key := range keys {
		grade := source[key]
		if grade == "S" || len(grade) == 1 && grade[0] >= 'A' && grade[0] <= 'G' {
			out[key] = grade
		}
	}
	return out
}
