#!/usr/bin/env bash

set -euo pipefail

version_digest_for_tag() {
	local name=$1
	local owner="${GITHUB_REPOSITORY%%/*}"
	local repository="${GITHUB_REPOSITORY#*/}"
	local package encoded versions
	package="${repository}/${name}"
	encoded="$(jq -rn --arg package "${package}" '$package | @uri')"
	versions="$(
		gh api --paginate --slurp \
			"/orgs/${owner}/packages/container/${encoded}/versions?per_page=100"
	)" || return 1
	jq -er \
		--arg tag "${VERSION}" '
            [
              flatten
              | .[]
              | select(.metadata.container.tags | index($tag))
            ]
            | if length == 0 then ""
              elif length == 1 and .[0].metadata.container.tags == [$tag] then .[0].name
              elif length == 1 then error("release package version carries another tag")
              else error("more than one package version carries the release tag")
              end
          ' <<<"${versions}"
}

promote_one() {
	local name=$1
	local exact_ref existing_digest image index_file resolved_digest
	local source_digest source_image_file version_digest
	exact_ref="$(<"assets/${name}.source.digest.txt")"
	image="${exact_ref%@*}"
	source_digest="${exact_ref#*@}"
	index_file="${RUNNER_TEMP}/${name}.index.json"
	source_image_file="${RUNNER_TEMP}/${name}.source-image.json"
	docker buildx imagetools inspect "${exact_ref}" \
		--format '{{json .Image}}' >"${source_image_file}" || return 1

	existing_digest="$(version_digest_for_tag "${name}")" || return 1
	if [[ -n ${existing_digest} ]]; then
		version_digest="${existing_digest}"
		docker buildx imagetools inspect "${image}:${VERSION}" --raw \
			>"${index_file}" || return 1
		resolved_digest="$(
			docker buildx imagetools inspect "${image}:${VERSION}" \
				--format '{{json .Manifest}}' |
				jq -er .digest
		)" || return 1
		[[ ${resolved_digest} == "${version_digest}" ]] || return 1
	else
		if ! docker buildx imagetools create \
			--prefer-index=true \
			--annotation "index:org.opencontainers.image.revision=${SHA}" \
			--annotation "index:org.opencontainers.image.version=${VERSION}" \
			--tag "${image}:${VERSION}" \
			"${exact_ref}"; then
			# A lost registry acknowledgement can report failure after the tag
			# was committed. Reconciliation rediscovers registry state safely.
			return 1
		fi
		version_digest=
		for _ in {1..10}; do
			version_digest="$(
				docker buildx imagetools inspect "${image}:${VERSION}" \
					--format '{{json .Manifest}}' 2>/dev/null |
					jq -er .digest
			)" && break
			sleep 2
		done
		[[ ${version_digest} =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
		docker buildx imagetools inspect "${image}:${VERSION}" --raw \
			>"${index_file}" || return 1
	fi

	[[ ${source_digest} =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
	[[ ${version_digest} =~ ^sha256:[0-9a-f]{64}$ && ${version_digest} != "${source_digest}" ]] || return 1
	tools/bin/release-reconcile \
		--validate-index-only \
		--index "${index_file}" \
		--source-image "${source_image_file}" \
		--version "${VERSION}" \
		--sha "${SHA}" \
		--source-digest "${source_digest}" \
		>/dev/null || return 1
	echo "${image}@${version_digest}" >"${name}.digest.txt" || return 1
}

main() {
	local current_main
	current_main="$(gh api "repos/${GITHUB_REPOSITORY}/commits/main" --jq .sha)"
	if [[ ${current_main} != "${SHA}" ]]; then
		echo "::error::main advanced after qualification; no version tag was written."
		return 1
	fi

	promote_one agent || return 1
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
	main "$@"
fi
