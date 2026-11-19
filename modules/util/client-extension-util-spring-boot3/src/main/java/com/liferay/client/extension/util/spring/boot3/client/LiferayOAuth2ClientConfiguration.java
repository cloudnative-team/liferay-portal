/**
 * SPDX-FileCopyrightText: (c) 2025 Liferay, Inc. https://liferay.com
 * SPDX-License-Identifier: LGPL-2.1-or-later OR LicenseRef-Liferay-DXP-EULA-2.0.0-2023-06
 */

package com.liferay.client.extension.util.spring.boot3.client;

import com.nimbusds.jose.JOSEException;
import com.nimbusds.jose.jwk.JWK;
import com.nimbusds.jose.jwk.RSAKey;

import java.io.IOException;

import java.math.BigInteger;

import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

import java.security.GeneralSecurityException;
import java.security.KeyFactory;
import java.security.interfaces.RSAPrivateCrtKey;
import java.security.interfaces.RSAPublicKey;
import java.security.spec.PKCS8EncodedKeySpec;
import java.security.spec.RSAPublicKeySpec;

import java.util.ArrayList;
import java.util.Base64;
import java.util.Collections;
import java.util.List;

import org.apache.commons.logging.Log;
import org.apache.commons.logging.LogFactory;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.Primary;
import org.springframework.core.env.Environment;
import org.springframework.security.oauth2.client.AuthorizedClientServiceOAuth2AuthorizedClientManager;
import org.springframework.security.oauth2.client.AuthorizedClientServiceReactiveOAuth2AuthorizedClientManager;
import org.springframework.security.oauth2.client.InMemoryOAuth2AuthorizedClientService;
import org.springframework.security.oauth2.client.InMemoryReactiveOAuth2AuthorizedClientService;
import org.springframework.security.oauth2.client.OAuth2AuthorizedClientProviderBuilder;
import org.springframework.security.oauth2.client.OAuth2AuthorizedClientService;
import org.springframework.security.oauth2.client.ReactiveOAuth2AuthorizedClientProviderBuilder;
import org.springframework.security.oauth2.client.endpoint.NimbusJwtClientAuthenticationParametersConverter;
import org.springframework.security.oauth2.client.endpoint.OAuth2ClientCredentialsGrantRequest;
import org.springframework.security.oauth2.client.endpoint.RestClientClientCredentialsTokenResponseClient;
import org.springframework.security.oauth2.client.endpoint.WebClientReactiveClientCredentialsTokenResponseClient;
import org.springframework.security.oauth2.client.registration.ClientRegistration;
import org.springframework.security.oauth2.client.registration.ClientRegistrationRepository;
import org.springframework.security.oauth2.client.registration.InMemoryClientRegistrationRepository;
import org.springframework.security.oauth2.client.registration.InMemoryReactiveClientRegistrationRepository;
import org.springframework.security.oauth2.client.registration.ReactiveClientRegistrationRepository;
import org.springframework.security.oauth2.client.web.reactive.function.client.ServerOAuth2AuthorizedClientExchangeFilterFunction;
import org.springframework.security.oauth2.core.AuthorizationGrantType;
import org.springframework.security.oauth2.core.ClientAuthenticationMethod;
import org.springframework.web.reactive.function.client.WebClient;

import reactor.core.publisher.Mono;

/**
 * @author Gregory Amerson
 */
@Configuration
public class LiferayOAuth2ClientConfiguration {

	@Bean
	public AuthorizedClientServiceOAuth2AuthorizedClientManager
		authorizedClientServiceOAuth2AuthorizedClientManager(
			ClientRegistrationRepository clientRegistrationRepository,
			OAuth2AuthorizedClientService oAuth2AuthorizedClientService) {

		AuthorizedClientServiceOAuth2AuthorizedClientManager
			authorizedClientServiceOAuth2AuthorizedClientManager =
				new AuthorizedClientServiceOAuth2AuthorizedClientManager(
					clientRegistrationRepository,
					oAuth2AuthorizedClientService);

		RestClientClientCredentialsTokenResponseClient
			restClientClientCredentialsTokenResponseClient =
				new RestClientClientCredentialsTokenResponseClient();

		restClientClientCredentialsTokenResponseClient.addParametersConverter(
			new NimbusJwtClientAuthenticationParametersConverter<>(
				this::_getJWK));

		authorizedClientServiceOAuth2AuthorizedClientManager.
			setAuthorizedClientProvider(
				OAuth2AuthorizedClientProviderBuilder.builder(
				).clientCredentials(
					clientCredentialsGrantBuilder ->
						clientCredentialsGrantBuilder.accessTokenResponseClient(
							restClientClientCredentialsTokenResponseClient)
				).build());

		return authorizedClientServiceOAuth2AuthorizedClientManager;
	}

	@Bean
	@Primary
	public ClientRegistrationRepository clientRegistrationRepository() {
		List<ClientRegistration> clientRegistrations =
			_getClientRegistrations();

		if (_extraInMemoryClientRegistrationRepository != null) {
			_extraInMemoryClientRegistrationRepository.forEach(
				clientRegistrations::add);
		}

		if (!clientRegistrations.isEmpty()) {
			return new InMemoryClientRegistrationRepository(
				clientRegistrations.toArray(new ClientRegistration[0]));
		}

		return new InMemoryClientRegistrationRepository(Collections.emptyMap());
	}

	@Bean
	@Primary
	public ReactiveClientRegistrationRepository clientRegistrations() {
		List<ClientRegistration> clientRegistrations =
			_getClientRegistrations();

		if (_extraInMemoryReactiveClientRegistrationRepository != null) {
			_extraInMemoryReactiveClientRegistrationRepository.forEach(
				clientRegistrations::add);
		}

		if (!clientRegistrations.isEmpty()) {
			return new InMemoryReactiveClientRegistrationRepository(
				clientRegistrations.toArray(new ClientRegistration[0]));
		}

		return new ReactiveClientRegistrationRepository() {

			@Override
			public Mono<ClientRegistration> findByRegistrationId(
				String registrationId) {

				return Mono.empty();
			}

		};
	}

	@Bean
	public OAuth2AuthorizedClientService oAuth2AuthorizedClientService(
		ClientRegistrationRepository clientRegistrationRepository) {

		return new InMemoryOAuth2AuthorizedClientService(
			clientRegistrationRepository);
	}

	@Bean
	public WebClient webClient(
		ReactiveClientRegistrationRepository
			reactiveClientRegistrationRepository) {

		AuthorizedClientServiceReactiveOAuth2AuthorizedClientManager
			authorizedClientServiceReactiveOAuth2AuthorizedClientManager =
				new AuthorizedClientServiceReactiveOAuth2AuthorizedClientManager(
					reactiveClientRegistrationRepository,
					new InMemoryReactiveOAuth2AuthorizedClientService(
						reactiveClientRegistrationRepository));

		WebClientReactiveClientCredentialsTokenResponseClient
			webClientReactiveClientCredentialsTokenResponseClient =
				new WebClientReactiveClientCredentialsTokenResponseClient();

		webClientReactiveClientCredentialsTokenResponseClient.
			addParametersConverter(
				new NimbusJwtClientAuthenticationParametersConverter
					<OAuth2ClientCredentialsGrantRequest>(this::_getJWK));

		authorizedClientServiceReactiveOAuth2AuthorizedClientManager.
			setAuthorizedClientProvider(
				ReactiveOAuth2AuthorizedClientProviderBuilder.builder(
				).clientCredentials(
					clientCredentialsGrantBuilder ->
						clientCredentialsGrantBuilder.accessTokenResponseClient(
							webClientReactiveClientCredentialsTokenResponseClient)
				).build());

		return WebClient.builder(
		).filter(
			new ServerOAuth2AuthorizedClientExchangeFilterFunction(
				authorizedClientServiceReactiveOAuth2AuthorizedClientManager)
		).build();
	}

	private List<ClientRegistration> _getClientRegistrations() {
		String liferayOauthApplicationExternalReferenceCodes =
			_environment.getProperty(
				"liferay.oauth.application.external.reference.codes");

		if (liferayOauthApplicationExternalReferenceCodes == null) {
			throw new IllegalArgumentException(
				"Property " +
					"\"liferay.oauth.application.external.reference.codes\" " +
						"is not defined");
		}

		List<ClientRegistration> clientRegistrations = new ArrayList<>();

		for (String externalReferenceCode :
				liferayOauthApplicationExternalReferenceCodes.split(",")) {

			if (_log.isDebugEnabled()) {
				_log.debug(
					"Processing external reference code " +
						externalReferenceCode);
			}

			String clientId = _environment.getProperty(
				externalReferenceCode + ".oauth2.headless.server.client.id");

			if (clientId == null) {
				clientId = LiferayOAuth2Util.getClientId(
					externalReferenceCode, _lxcDXPMainDomain,
					_lxcDXPServerProtocol);
			}

			if (clientId == null) {
				if (_log.isDebugEnabled()) {
					_log.debug(
						"Unable to get client ID for external reference code " +
							externalReferenceCode);
				}

				continue;
			}

			if (_log.isDebugEnabled()) {
				_log.debug(
					new StringBuilder(
					).append(
						"Obtained client_id: "
					).append(
						clientId
					).append(
						" for externalReferenceCode "
					).append(
						externalReferenceCode
					));
			}

			String clientSecret = _environment.getProperty(
				externalReferenceCode +
					".oauth2.headless.server.client.secret");

			RSAKey rsaKey = null;

			if (_environment.containsProperty(
					externalReferenceCode +
						".oauth2.headless.server.client.id")) {

				rsaKey = _getRSAKey();
			}

			if ((clientSecret == null) && (rsaKey == null)) {
				if (_log.isDebugEnabled()) {
					_log.debug(
						"Unable to get client secret for external reference " +
							"code " + externalReferenceCode);
				}

				continue;
			}

			if (_log.isDebugEnabled()) {
				if (rsaKey != null) {
					_log.debug(
						"Using private key " + rsaKey.getKeyID() +
							" for external reference code " +
								externalReferenceCode);
				}
				else {
					_log.debug(
						"Obtained client_secret for external reference code " +
							externalReferenceCode);
				}
			}

			String tokenURI = _environment.getProperty(
				externalReferenceCode + ".oauth2.token.uri", "/o/oauth2/token");

			if (!tokenURI.contains("://")) {
				tokenURI = new StringBuilder(
				).append(
					_lxcDXPServerProtocol
				).append(
					"://"
				).append(
					_lxcDXPMainDomain
				).append(
					tokenURI
				).toString();
			}

			ClientRegistration.Builder builder =
				ClientRegistration.withRegistrationId(
					externalReferenceCode
				).tokenUri(
					tokenURI
				).clientId(
					clientId
				).authorizationGrantType(
					AuthorizationGrantType.CLIENT_CREDENTIALS
				);

			if (rsaKey != null) {
				builder.clientAuthenticationMethod(
					ClientAuthenticationMethod.PRIVATE_KEY_JWT);
			}
			else {
				builder.clientAuthenticationMethod(
					ClientAuthenticationMethod.CLIENT_SECRET_POST
				).clientSecret(
					clientSecret
				);
			}

			ClientRegistration clientRegistration = builder.build();

			if (_log.isDebugEnabled()) {
				_log.debug("Adding client registration " + clientRegistration);
			}

			clientRegistrations.add(clientRegistration);
		}

		return clientRegistrations;
	}

	private JWK _getJWK(ClientRegistration clientRegistration) {
		if (!ClientAuthenticationMethod.PRIVATE_KEY_JWT.equals(
				clientRegistration.getClientAuthenticationMethod())) {

			return null;
		}

		return _getRSAKey();
	}

	private synchronized RSAKey _getRSAKey() {
		if (_rsaKey != null) {
			return _rsaKey;
		}

		Path path = Paths.get(_privateKeyPath);

		if (!Files.isReadable(path)) {
			return null;
		}

		try {
			String pem = Files.readString(path);

			byte[] bytes = Base64.getMimeDecoder(
			).decode(
				pem.replaceAll("-----[A-Z ]+-----", "")
			);

			KeyFactory keyFactory = KeyFactory.getInstance("RSA");

			RSAPrivateCrtKey rsaPrivateCrtKey =
				(RSAPrivateCrtKey)keyFactory.generatePrivate(
					new PKCS8EncodedKeySpec(bytes));

			BigInteger modulus = rsaPrivateCrtKey.getModulus();

			RSAPublicKey rsaPublicKey =
				(RSAPublicKey)keyFactory.generatePublic(
					new RSAPublicKeySpec(
						modulus, rsaPrivateCrtKey.getPublicExponent()));

			_rsaKey = new RSAKey.Builder(
				rsaPublicKey
			).privateKey(
				rsaPrivateCrtKey
			).keyIDFromThumbprint(
			).build();

			if (_log.isInfoEnabled()) {
				_log.info(
					"Loaded private key " + _rsaKey.getKeyID() + " from " +
						path);
			}

			return _rsaKey;
		}
		catch (GeneralSecurityException | IOException | JOSEException
					exception) {

			throw new IllegalStateException(
				"Unable to load private key from " + path, exception);
		}
	}

	private static final Log _log = LogFactory.getLog(
		LiferayOAuth2ClientConfiguration.class);

	@Autowired
	private Environment _environment;

	@Autowired(required = false)
	@Qualifier("extra")
	private InMemoryClientRegistrationRepository
		_extraInMemoryClientRegistrationRepository;

	@Autowired(required = false)
	@Qualifier("extra")
	private InMemoryReactiveClientRegistrationRepository
		_extraInMemoryReactiveClientRegistrationRepository;

	@Value("${com.liferay.lxc.dxp.domains}")
	private String _lxcDXPDomains;

	@Value("${com.liferay.lxc.dxp.mainDomain}")
	private String _lxcDXPMainDomain;

	@Value("${com.liferay.lxc.dxp.server.protocol}")
	private String _lxcDXPServerProtocol;

	@Value(
		"${liferay.oauth.private.key.path:/etc/liferay/lxc/oauth2-private-key/private.key}"
	)
	private String _privateKeyPath;

	private RSAKey _rsaKey;

}