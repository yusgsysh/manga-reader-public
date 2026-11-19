package auth

import "github.com/golang-jwt/jwt/v5"

func methodNoneForTest() jwt.SigningMethod { return jwt.SigningMethodNone }
