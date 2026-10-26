{
	loc = $1
	sub(/:$/, "", loc)
	split(loc, p, ":")
	path = p[1]
	if (index(path, mod) == 1) {
		path = substr(path, length(mod) + 1)
	}

	text = path ":" p[2]
	if (base != "") {
		text = "[" text "](" base "/" path "#L" p[2] ")"
	}

	printf "| `%s` | %s | %s |\n", $2, text, $3
}
