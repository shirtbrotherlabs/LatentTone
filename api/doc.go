// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

// Package api holds the checked-in OpenAPI 3 artifact for Phase 3B.
package api

import _ "embed"

// OpenAPIYAML is the hand-maintained OpenAPI 3 description of Phase 2+3 APIs.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte
