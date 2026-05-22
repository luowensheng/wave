package routes

import (
	magic "github.com/luowensheng/wave/usecases/magic_link"
	totprt "github.com/luowensheng/wave/usecases/totp_routes"
)

// MagicLinkRequestConfig — POST /login/request {"email": "..."}
type MagicLinkRequestConfig = magic.RequestConfig

// MagicLinkConsumeConfig — GET /login/verify?token=...
type MagicLinkConsumeConfig = magic.ConsumeConfig

// TOTPEnrollStartConfig — POST /totp/enroll
type TOTPEnrollStartConfig = totprt.EnrollStartConfig

// TOTPEnrollConfirmConfig — POST /totp/confirm  (body: {"code": "123456"})
type TOTPEnrollConfirmConfig = totprt.EnrollConfirmConfig

// TOTPVerifyConfig — POST /totp/verify  (standalone 2FA)
type TOTPVerifyConfig = totprt.VerifyConfig
