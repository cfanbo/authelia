package handlers

import (
	"bytes"

	"github.com/duo-labs/webauthn/protocol"
	"github.com/duo-labs/webauthn/webauthn"
	"github.com/valyala/fasthttp"

	"github.com/authelia/authelia/v4/internal/middlewares"
	"github.com/authelia/authelia/v4/internal/models"
	"github.com/authelia/authelia/v4/internal/regulation"
)

// SecondFactorWebauthnIdentityStart the handler for initiating the identity validation.
var SecondFactorWebauthnIdentityStart = middlewares.IdentityVerificationStart(middlewares.IdentityVerificationStartArgs{
	MailTitle:             "Register your key",
	MailButtonContent:     "Register",
	TargetEndpoint:        "/webauthn/register",
	ActionClaim:           ActionWebauthnRegistration,
	IdentityRetrieverFunc: identityRetrieverFromSession,
}, nil)

// SecondFactorWebauthnIdentityFinish the handler for finishing the identity validation.
var SecondFactorWebauthnIdentityFinish = middlewares.IdentityVerificationFinish(
	middlewares.IdentityVerificationFinishArgs{
		ActionClaim:          ActionWebauthnRegistration,
		IsTokenUserValidFunc: isTokenUserValidFor2FARegistration,
	}, SecondFactorWebauthnAttestationGET)

// SecondFactorWebauthnAttestationGET returns the attestation challenge from the server.
func SecondFactorWebauthnAttestationGET(ctx *middlewares.AutheliaCtx, _ string) {
	var (
		w    *webauthn.WebAuthn
		user *models.WebauthnUser
		err  error
	)

	userSession := ctx.GetSession()

	if w, err = getWebauthn(ctx); err != nil {
		ctx.Logger.Errorf("Unable to create %s attestation challenge for user '%s': %+v", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageUnableToRegisterSecurityKey)

		return
	}

	if user, err = getWebAuthnUser(ctx, userSession); err != nil {
		ctx.Logger.Errorf("Unable to load %s devices for assertion challenge for user '%s': %+v", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageMFAValidationFailed)

		return
	}

	var credentialCreation *protocol.CredentialCreation

	if credentialCreation, userSession.Webauthn, err = w.BeginRegistration(user); err != nil {
		ctx.Logger.Errorf("Unable to create %s attestation challenge for user '%s': %+v", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageUnableToRegisterSecurityKey)

		return
	}

	if err = ctx.SaveSession(userSession); err != nil {
		ctx.Logger.Errorf(logFmtErrSessionSave, "attestation challenge", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageUnableToRegisterSecurityKey)

		return
	}

	if err = ctx.SetJSONBody(credentialCreation); err != nil {
		ctx.Logger.Errorf(logFmtErrWriteResponseBody, regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageUnableToRegisterSecurityKey)

		return
	}
}

// SecondFactorWebauthnAttestationPOST processes the attestation challenge response from the client.
func SecondFactorWebauthnAttestationPOST(ctx *middlewares.AutheliaCtx) {
	var (
		err  error
		w    *webauthn.WebAuthn
		user *models.WebauthnUser

		attestationResponse *protocol.ParsedCredentialCreationData
		credential          *webauthn.Credential
	)

	userSession := ctx.GetSession()

	if w, err = getWebauthn(ctx); err != nil {
		ctx.Logger.Errorf("Unable to configire %s during assertion challenge for user '%s': %+v", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageUnableToRegisterSecurityKey)

		return
	}

	if attestationResponse, err = protocol.ParseCredentialCreationResponseBody(bytes.NewReader(ctx.PostBody())); err != nil {
		ctx.Logger.Errorf("Unable to parse %s assertionfor user '%s': %+v", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageMFAValidationFailed)

		return
	}

	if user, err = getWebAuthnUser(ctx, userSession); err != nil {
		ctx.Logger.Errorf("Unable to load %s devices for assertion challenge for user '%s': %+v", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageMFAValidationFailed)

		return
	}

	if credential, err = w.CreateCredential(user, *userSession.Webauthn, attestationResponse); err != nil {
		ctx.Logger.Errorf("Unable to load %s devices for assertion challenge for user '%s': %+v", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageMFAValidationFailed)

		return
	}

	device := models.NewWebauthnDeviceFromCredential(userSession.Username, "Primary", ctx.RemoteIP(), credential)

	if err = ctx.Providers.StorageProvider.SaveWebauthnDevice(ctx, device); err != nil {
		ctx.Logger.Errorf("Unable to load %s devices for assertion challenge for user '%s': %+v", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageMFAValidationFailed)

		return
	}

	userSession.Webauthn = nil
	if err = ctx.SaveSession(userSession); err != nil {
		ctx.Logger.Errorf(logFmtErrSessionSave, "removal of the attestation challenge", regulation.AuthTypeWebauthn, userSession.Username, err)

		respondUnauthorized(ctx, messageMFAValidationFailed)

		return
	}

	ctx.ReplyOK()
	ctx.SetStatusCode(fasthttp.StatusCreated)
}