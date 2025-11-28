package schema

func WrapTx(statements []string) string {
	if len(statements) == 0 {
		return ""
	}

	content := "BEGIN;\n\n"
	for _, stmt := range statements {
		if stmt != "" {
			content += stmt + ";\n"
		}
	}
	content += "\nCOMMIT;"
	return content
}
