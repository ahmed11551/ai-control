package schema

import _ "embed"

//go:embed local.sql
var LocalSQL string

//go:embed compat.sql
var CompatSQL string

//go:embed import.sql
var ImportSQL string
