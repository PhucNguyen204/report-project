package observability

func JobKinds() []string {
	return []string{"email", "notification", "settings.changed", "space.copy", "space.purge", "account.purge", "account.anonymized", "food.merged", "config.changed", "report.export", "media.delete", "export.delete", "assignment.digest", "ai.suggest", "other"}
}

func JobKind(kind string) string {
	for _, known := range JobKinds() {
		if known == kind {
			return kind
		}
	}
	return "other"
}
