/**
 * SPDX-FileCopyrightText: (c) 2026 Liferay, Inc. https://liferay.com
 * SPDX-License-Identifier: LGPL-2.1-or-later OR LicenseRef-Liferay-DXP-EULA-2.0.0-2023-06
 */

package com.liferay.gradle.plugins.workspace.internal.client.extension;

import java.util.Map;

import org.junit.Assert;
import org.junit.Test;

/**
 * @author Gregory Amerson
 */
public class ClientExtensionDescriptorTest {

	@Test
	public void testDerive() {
		Map<String, Object> clientExtensionMap = _derive(
			ClientExtensionDescriptor.TYPE_SERVICE, "able-oauth");

		Assert.assertEquals(
			Map.of("key~able", Map.of()), clientExtensionMap.get("configs"));
		Assert.assertEquals(
			ClientExtensionDescriptor.VERSION,
			clientExtensionMap.get("descriptorVersion"));
		Assert.assertEquals(
			Map.of(
				"digest", "sha256:1", "repository", "registry/able", "tag",
				"1.0.0"),
			clientExtensionMap.get("image"));
		Assert.assertNull(
			clientExtensionMap.get("oauthApplicationHeadlessServerERC"));
		Assert.assertNull(clientExtensionMap.get("serviceId"));
		Assert.assertEquals(
			ClientExtensionDescriptor.TYPE_SERVICE,
			clientExtensionMap.get("type"));
		Assert.assertNull(clientExtensionMap.get("workload"));
	}

	@Test
	public void testDeriveWhenIsBatch() {
		Map<String, Object> clientExtensionMap = _derive(
			ClientExtensionDescriptor.TYPE_BATCH, "able-oauth");

		Assert.assertNotNull(clientExtensionMap.get("image"));
		Assert.assertEquals(
			"able-oauth",
			clientExtensionMap.get("oauthApplicationHeadlessServerERC"));
		Assert.assertEquals(
			ClientExtensionDescriptor.TYPE_BATCH,
			clientExtensionMap.get("type"));
	}

	@Test
	public void testDeriveWhenIsConfigurationOnly() {
		Map<String, Object> clientExtensionMap = _derive(
			ClientExtensionDescriptor.TYPE_CONFIGURATION, "able-oauth");

		Assert.assertNull(clientExtensionMap.get("image"));
		Assert.assertNull(
			clientExtensionMap.get("oauthApplicationHeadlessServerERC"));
		Assert.assertEquals(
			ClientExtensionDescriptor.TYPE_CONFIGURATION,
			clientExtensionMap.get("type"));
		Assert.assertNull(clientExtensionMap.get("workload"));
	}

	@Test
	public void testGetRepository() {
		Assert.assertEquals(
			"localhost:5001/able",
			ClientExtensionDescriptor.getRepository(
				"localhost:5001/able:1.0.0@sha256:1"));
		Assert.assertEquals(
			"localhost:5001/able",
			ClientExtensionDescriptor.getRepository("localhost:5001/able"));
	}

	@Test
	public void testGetType() {
		Assert.assertEquals(
			ClientExtensionDescriptor.TYPE_BATCH,
			ClientExtensionDescriptor.getType("batch"));
		Assert.assertEquals(
			ClientExtensionDescriptor.TYPE_SERVICE,
			ClientExtensionDescriptor.getType("microservice"));
	}

	private Map<String, Object> _derive(
		String type, String oAuthApplicationHeadlessServerERC) {

		Map<String, Object> descriptorMap = ClientExtensionDescriptor.derive(
			type, Map.of("key~able", Map.of()),
			oAuthApplicationHeadlessServerERC, "sha256:1", "registry/able",
			"1.0.0");

		Assert.assertEquals(descriptorMap.toString(), 1, descriptorMap.size());

		return (Map<String, Object>)descriptorMap.get("descriptor");
	}

}