package datamodeling

import "megane/internal/db"

func CalculateConvergenceScore(database *db.DB, fileTypeID int64) (float64, error) {
	metrics := []string{"field_stability", "confidence_trend", "new_field_rate", "type_consistency"}
	values := make([]float64, len(metrics))

	for i, m := range metrics {
		data, err := GetRecentMetrics(database, fileTypeID, m, 5)
		if err != nil {
			return 0, err
		}
		if len(data) < 2 {
			values[i] = 0.0
			continue
		}
		var sum float64
		for _, v := range data {
			sum += v
		}
		values[i] = sum / float64(len(data))
	}

	// new_field_rate contributes as 1 - avg
	score := (values[0] + values[1] + (1.0 - values[2]) + values[3]) / 4.0
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score, nil
}

func RecordSchemaMetrics(database *db.DB, fileTypeID int64,
	prev *ProposedSchema, merged ProposedSchema) (float64, error) {

	mergedNames := map[string]bool{}
	for _, f := range merged.Fields {
		if !f.Deprecated {
			mergedNames[f.Name] = true
		}
	}

	var prevNames map[string]bool
	if prev != nil {
		prevNames = map[string]bool{}
		for _, f := range prev.Fields {
			if !f.Deprecated {
				prevNames[f.Name] = true
			}
		}
	}

	// field_stability
	fieldStability := 1.0
	if prev != nil && len(mergedNames) > 0 {
		intersection := 0
		for name := range mergedNames {
			if prevNames[name] {
				intersection++
			}
		}
		fieldStability = float64(intersection) / float64(len(mergedNames))
	}

	// confidence_trend
	confidenceTrend := 0.0
	if len(mergedNames) > 0 {
		high := 0
		for _, f := range merged.Fields {
			if !f.Deprecated && (f.ExtractionConfidence == "high" || f.ExtractionConfidence == "medium") {
				high++
			}
		}
		confidenceTrend = float64(high) / float64(len(mergedNames))
	}

	// new_field_rate
	newFieldRate := 0.0
	if prev != nil && len(mergedNames) > 0 {
		added := 0
		for name := range mergedNames {
			if !prevNames[name] {
				added++
			}
		}
		newFieldRate = float64(added) / float64(len(mergedNames))
	}

	// type_consistency
	typeConsistency := 1.0
	if prev != nil {
		prevTypes := map[string]string{}
		for _, f := range prev.Fields {
			prevTypes[f.Name] = f.Type
		}
		consistent := 0
		both := 0
		for _, f := range merged.Fields {
			if f.Deprecated {
				continue
			}
			if pt, ok := prevTypes[f.Name]; ok {
				both++
				if pt == f.Type {
					consistent++
				}
			}
		}
		if both > 0 {
			typeConsistency = float64(consistent) / float64(both)
		}
	}

	for metricType, value := range map[string]float64{
		"field_stability":  fieldStability,
		"confidence_trend": confidenceTrend,
		"new_field_rate":   newFieldRate,
		"type_consistency": typeConsistency,
	} {
		if err := RecordConvergenceMetric(database, fileTypeID, metricType, value); err != nil {
			return 0, err
		}
	}

	return CalculateConvergenceScore(database, fileTypeID)
}

func RecommendationFromScore(score float64) string {
	switch {
	case score < 0.4:
		return "needs_more_samples"
	case score < 0.7:
		return "continue_with_more_samples"
	case score < 0.9:
		return "ready_for_review"
	default:
		return "ready_for_production"
	}
}

func BuildConvergenceIndicators(database *db.DB, fileTypeID int64,
	score float64, mergedSchema ProposedSchema) (ConvergenceIndicators, error) {

	filesAnalyzed, err := CountAnalysesByFileType(database, fileTypeID)
	if err != nil {
		return ConvergenceIndicators{}, err
	}

	total := 0
	highMed := 0
	for _, f := range mergedSchema.Fields {
		if f.Deprecated {
			continue
		}
		total++
		if f.ExtractionConfidence == "high" || f.ExtractionConfidence == "medium" {
			highMed++
		}
	}
	avgConf := 0.0
	if total > 0 {
		avgConf = float64(highMed) / float64(total)
	}

	return ConvergenceIndicators{
		SchemaStabilityScore: score,
		AverageConfidence:    avgConf,
		FilesAnalyzed:        filesAnalyzed,
		Recommendation:       RecommendationFromScore(score),
	}, nil
}
