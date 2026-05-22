package routes

import "wave/usecases/match"

// MatchConfig is the YAML shape for `type: match` routes.
// See usecases/match for documentation.
type MatchConfig = match.Config
