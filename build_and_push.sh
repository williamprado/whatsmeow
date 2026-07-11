#!/bin/bash
## ============================================================================
## WHATSMEOW (fork) - Build e Push da imagem Docker da API VoIP
## ============================================================================
## Mesma logica do atendzappy_versao2/build_and_push.sh, adaptada para uma
## unica imagem (voip-bridge-server). Docker Hub e a unica referencia de
## versionamento: descobre a maior tag vX.Y.Z publicada, incrementa o patch,
## builda e publica a tag versionada e depois latest.
##
## Executar num host com Docker logado no Docker Hub (ou via GitHub Actions).
## Uso: bash build_and_push.sh
## ============================================================================

set -euo pipefail

## ========================= CONFIGURACOES ========================= ##

DOCKER_USER="${DOCKER_USERNAME:-williamwilmer10}"
REPOSITORY="${WHATSMEOW_REPOSITORY:-atendzappy-whatsmeow}"
IMAGE="${DOCKER_USER}/${REPOSITORY}"
MIN_VERSION="1.0.0"
VERSION_FILE=".docker_version"
DOCKER_HUB_PAGE_SIZE="${DOCKER_HUB_PAGE_SIZE:-100}"
DOCKER_HUB_MAX_PAGES="${DOCKER_HUB_MAX_PAGES:-50}"

## ========================= CORES PARA OUTPUT ========================= ##

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

## ========================= FUNCOES ========================= ##

version_max() {
    printf "%s\n" "$@" \
        | sed '/^$/d' \
        | sed 's/^v//' \
        | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' \
        | sort -V \
        | tail -1 || true
}

increment_patch_version() {
    local version="$1"
    local major minor patch

    IFS='.' read -r major minor patch <<< "$version"
    echo "${major}.${minor}.$((patch + 1))"
}

fetch_dockerhub_versions() {
    local page=1
    local payload names

    if ! command -v curl >/dev/null 2>&1; then
        return 0
    fi

    while [ "$page" -le "$DOCKER_HUB_MAX_PAGES" ]; do
        payload=$(
            curl -fsSL \
                "https://hub.docker.com/v2/namespaces/${DOCKER_USER}/repositories/${REPOSITORY}/tags?page_size=${DOCKER_HUB_PAGE_SIZE}&page=${page}" \
                2>/dev/null
        ) || return 0

        names=$(
            printf "%s" "$payload" \
                | tr -d '\r\n' \
                | grep -oE '"name"[[:space:]]*:[[:space:]]*"v[0-9]+\.[0-9]+\.[0-9]+"' \
                | sed -E 's/.*"v([^"]+)".*/\1/' || true
        )

        [ -n "$names" ] || break
        printf "%s\n" "$names"

        page=$((page + 1))
    done
}

dockerhub_tag_exists() {
    local tag="$1"
    local http_code

    if ! command -v curl >/dev/null 2>&1; then
        return 1
    fi

    http_code=$(
        curl -fsS -o /dev/null -w "%{http_code}" \
            "https://hub.docker.com/v2/namespaces/${DOCKER_USER}/repositories/${REPOSITORY}/tags/${tag}" \
            2>/dev/null || true
    )

    [ "$http_code" = "200" ]
}

# Verifica direto no REGISTRY (registry-1.docker.io, autenticado): reflete o
# push na hora. A API do hub.docker.com e eventualmente consistente e sofre
# rate-limit (licao aprendida no atendzappy: digest publicado e a API demorou
# >120s para expor a tag, derrubando o job com o push ja concluido).
registry_tag_exists() {
    local tag="$1"

    docker manifest inspect "${IMAGE}:${tag}" >/dev/null 2>&1
}

wait_for_dockerhub_tag() {
    local tag="$1"
    local max_attempts="${2:-12}"
    local sleep_seconds="${3:-10}"
    local attempt

    for attempt in $(seq 1 "$max_attempts"); do
        if registry_tag_exists "$tag"; then
            echo -e "${GREEN}OK: Tag confirmada no registry: ${REPOSITORY}:${tag}${NC}"
            return 0
        fi
        if dockerhub_tag_exists "$tag"; then
            echo -e "${GREEN}OK: Tag disponivel no Docker Hub: ${REPOSITORY}:${tag}${NC}"
            return 0
        fi
        echo -e "${YELLOW}[!] Tag ainda nao visivel (registry/API): ${REPOSITORY}:${tag} (tentativa ${attempt}/${max_attempts}, aguardando ${sleep_seconds}s...)${NC}"
        sleep "$sleep_seconds"
    done

    echo -e "${RED}ERRO: Tag ${REPOSITORY}:${tag} nao ficou disponivel apos $((max_attempts * sleep_seconds))s.${NC}"
    return 1
}

ensure_dockerhub_authenticated() {
    local docker_config="${DOCKER_CONFIG:-$HOME/.docker}"

    if docker info 2>/dev/null | grep -qi "Username"; then
        return 0
    fi

    if [ -f "${docker_config}/config.json" ] && grep -q '"auths"' "${docker_config}/config.json"; then
        return 0
    fi

    if [ -n "${DOCKER_USERNAME:-}" ] && [ -n "${DOCKER_PASSWORD:-}" ]; then
        echo "${DOCKER_PASSWORD}" | docker login -u "${DOCKER_USERNAME}" --password-stdin >/dev/null 2>&1
        return $?
    fi

    return 1
}

resolve_current_version() {
    local remote_version

    remote_version=$(version_max "$(fetch_dockerhub_versions)")

    if [ -n "$remote_version" ]; then
        echo -e "${YELLOW}[!] Ultima tag no Docker Hub: v${remote_version}${NC}" >&2
        version_max "$MIN_VERSION" "$remote_version"
        return
    fi

    ## Repositorio novo (ou sem tags vX.Y.Z): bootstrap a partir da MIN_VERSION.
    echo -e "${YELLOW}[!] Nenhuma tag vX.Y.Z no Docker Hub para ${REPOSITORY}; iniciando em v${MIN_VERSION}.${NC}" >&2
    echo "$MIN_VERSION"
}

ensure_tag_is_new() {
    local tag="$1"

    if dockerhub_tag_exists "$tag"; then
        echo -e "${RED}ERRO: A tag ${tag} ja existe no Docker Hub. Rode o script novamente para calcular a proxima versao.${NC}"
        exit 1
    fi
}

find_next_available_tag() {
    local current_version next_version candidate_tag

    current_version=$(resolve_current_version)

    while true; do
        next_version=$(increment_patch_version "$current_version")
        candidate_tag="v${next_version}"

        if ! dockerhub_tag_exists "$candidate_tag"; then
            NEXT_VERSION="$next_version"
            TAG="$candidate_tag"
            return
        fi

        current_version="$next_version"
    done
}

retag_built_image_if_needed() {
    local previous_tag="$TAG"

    if ! dockerhub_tag_exists "$TAG"; then
        return
    fi

    echo -e "${YELLOW}[!] A tag ${TAG} apareceu no Docker Hub durante o build.${NC}"
    echo -e "${YELLOW}[!] Recalculando proxima tag livre sem reconstruir a imagem...${NC}"

    find_next_available_tag

    echo -e "${YELLOW}[!] Retagueando imagem local: ${previous_tag} -> ${TAG}${NC}"

    docker tag "${IMAGE}:${previous_tag}" "${IMAGE}:${TAG}"
    docker tag "${IMAGE}:${previous_tag}" "${IMAGE}:latest"

    echo -e "${GREEN}OK: Build reaproveitado para a nova TAG: ${TAG}${NC}"
}

## ========================= INICIO ========================= ##

echo -e "${BLUE}============================================${NC}"
echo -e "${BLUE}  WHATSMEOW - Build & Push Docker Image     ${NC}"
echo -e "${BLUE}============================================${NC}"
echo ""

## ========================= PRE-REQUISITOS ========================= ##

echo -e "${YELLOW}[1/6] Verificando pre-requisitos...${NC}"

if ! command -v docker &> /dev/null; then
    echo -e "${RED}ERRO: Docker nao encontrado. Instale o Docker primeiro.${NC}"
    exit 1
fi

echo -e "${GREEN}OK: Docker encontrado${NC}"

if ! ensure_dockerhub_authenticated; then
    echo -e "${RED}ERRO: Voce nao esta logado no Docker Hub. Execute 'docker login' primeiro.${NC}"
    exit 1
fi

echo -e "${GREEN}OK: Docker Hub autenticado${NC}"
echo ""

## ========================= VERSIONAMENTO ========================= ##

echo -e "${YELLOW}[2/6] Calculando proxima versao...${NC}"

CURRENT_VERSION=$(resolve_current_version)
NEXT_VERSION=$(increment_patch_version "$CURRENT_VERSION")
TAG="v$NEXT_VERSION"

echo -e "${YELLOW}[!] Ultima versao encontrada: v${CURRENT_VERSION}${NC}"
echo -e "${GREEN}OK: Preparando build para a TAG: ${TAG}${NC}"

ensure_tag_is_new "$TAG"

echo ""

## ========================= BUILD ========================= ##

echo -e "${YELLOW}[3/6] Construindo imagem da API VoIP...${NC}"
echo -e "       Imagem: ${IMAGE}:${TAG}"
echo ""

## Contexto = raiz do repo: o modulo voip/ consome o whatsmeow deste checkout
## via replace => ../
docker build \
    -t "${IMAGE}:${TAG}" \
    -t "${IMAGE}:latest" \
    -f Dockerfile \
    .

echo ""
echo -e "${GREEN}OK: Imagem construida com sucesso!${NC}"
echo ""

## Revalida antes do push para reduzir risco de sobrescrever tag criada por
## outro build concorrente. Se a tag apareceu durante o build, reaproveita a
## imagem local e avanca para a proxima tag livre.
retag_built_image_if_needed

## ========================= PUSH TAG VERSIONADA ========================= ##

echo -e "${YELLOW}[4/6] Enviando tag versionada para Docker Hub...${NC}"

docker push "${IMAGE}:${TAG}"

if ! wait_for_dockerhub_tag "$TAG"; then
    echo -e "${RED}ERRO: Tag ${TAG} nao ficou disponivel no Docker Hub apos retries.${NC}"
    echo -e "${RED}ERRO: Nao atualizaremos latest nem ${VERSION_FILE}. Verifique manualmente o Docker Hub.${NC}"
    exit 1
fi

echo -e "${GREEN}OK: Tag versionada enviada!${NC}"
echo ""

## ========================= PUSH LATEST ========================= ##

echo -e "${YELLOW}[5/6] Atualizando tag latest no Docker Hub...${NC}"

docker push "${IMAGE}:latest"

echo -e "${GREEN}OK: Tag latest atualizada!${NC}"
echo ""

## Persistir somente depois que tudo foi publicado com sucesso.
echo "$NEXT_VERSION" > "$VERSION_FILE"

## ========================= RESUMO ========================= ##

echo -e "${YELLOW}[6/6] Resumo${NC}"
echo -e "${BLUE}============================================${NC}"
echo -e "${GREEN}Imagem construida e publicada:${NC}"
echo -e "   - ${IMAGE}:${TAG}"
echo -e "   - ${IMAGE}:latest"
echo ""
echo -e "${BLUE}Proximos passos (deploy manual, ver docs/voip_bridge.md):${NC}"
echo -e "   docker run -d -p 8080:8080 -v whatsmeow_data:/data \\"
echo -e "     -e VOIP_ENABLED=1 -e AUTH_TOKEN=<token> ${IMAGE}:${TAG}"
echo ""
echo -e "${YELLOW}⚠️  VoIP tem ALTISSIMO risco de ban: apenas contas descartaveis.${NC}"
echo -e "${BLUE}============================================${NC}"
