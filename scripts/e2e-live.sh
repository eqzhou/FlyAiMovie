#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8088/api/v1}"
EMAIL="${E2E_EMAIL:-e2e-local@flyaimovie.test}"
: "${E2E_PASSWORD:?set E2E_PASSWORD to a disposable 12+ character password}"
if [[ "${E2E_DISPOSABLE:-}" != "1" ]]; then
  echo "refusing to run: set E2E_DISPOSABLE=1 only for an empty disposable database" >&2
  exit 2
fi
COOKIE_JAR="$(mktemp)"
CSRF=""
ORG_SLUG=""
CREATED_ORG=0

cleanup() {
  if [[ "$CREATED_ORG" == "1" && -n "$CSRF" && -n "$ORG_SLUG" ]]; then
    curl -fsS -b "$COOKIE_JAR" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
      -X DELETE "$BASE_URL/organization" \
      -d "$(jq -nc --arg password "$E2E_PASSWORD" --arg confirmation "$ORG_SLUG" '{password:$password,confirmation:$confirmation}')" >/dev/null || true
  fi
  command rm -f "$COOKIE_JAR"
}
trap cleanup EXIT

json_request() {
  local method="$1" path="$2" body="${3:-}"
  local args=(-fsS -b "$COOKIE_JAR" -c "$COOKIE_JAR" -H 'Content-Type: application/json' -X "$method")
  if [[ -n "$CSRF" && "$method" != "GET" ]]; then args+=(-H "X-CSRF-Token: $CSRF"); fi
  if [[ -n "$body" ]]; then args+=(-d "$body"); fi
  curl "${args[@]}" "$BASE_URL$path"
}

data() { jq -e '.data'; }
field() { jq -er ".data.$1"; }

status="$(json_request GET /auth/status)"
if [[ "$(jq -r '.data.setup_required' <<<"$status")" != "true" ]]; then
  echo "live E2E requires an empty installation; use a disposable database" >&2
  exit 2
fi

setup_body="$(jq -nc --arg email "$EMAIL" --arg password "$E2E_PASSWORD" '{organization_name:"FlyAiMovie E2E",display_name:"E2E Owner",email:$email,password:$password}')"
setup="$(json_request POST /auth/setup "$setup_body")"
CSRF="$(jq -er '.data.csrf_token' <<<"$setup")"
ORG_SLUG="$(jq -er '.data.organization.slug' <<<"$setup")"
CREATED_ORG=1

configs="$(json_request GET /ai-configs | data)"
agents="$(json_request GET /agent-configs | data)"
if [[ "$(jq '[.[] | select(.provider == "mock")] | length' <<<"$configs")" != "4" ]]; then
  echo "expected exactly 4 organization-scoped Mock service configurations" >&2
  exit 1
fi
if [[ "$(jq 'length' <<<"$agents")" != "5" ]]; then
  echo "expected exactly 5 organization-scoped Agent configurations" >&2
  exit 1
fi
config_id() { jq -er --arg type "$1" '.[] | select(.provider=="mock" and .service_type==$type) | .id' <<<"$configs" | head -1; }
IMAGE_CONFIG="$(config_id image)"
VIDEO_CONFIG="$(config_id video)"
AUDIO_CONFIG="$(config_id audio)"

drama="$(json_request POST /dramas '{"title":"真实链路验收","total_episodes":1}')"
DRAMA_ID="$(field id <<<"$drama")"
episode_body="$(jq -nc --argjson drama "$DRAMA_ID" --argjson image "$IMAGE_CONFIG" --argjson video "$VIDEO_CONFIG" --argjson audio "$AUDIO_CONFIG" '{drama_id:$drama,title:"第一集",image_config_id:$image,video_config_id:$video,audio_config_id:$audio}')"
episode="$(json_request POST /episodes "$episode_body")"
EPISODE_ID="$(field id <<<"$episode")"
storyboard_body="$(jq -nc --argjson episode "$EPISODE_ID" '{episode_id:$episode,title:"开场",image_prompt:"quiet room",video_prompt:"quiet room",dialogue:"阿宁：你好",duration:1}')"
storyboard="$(json_request POST /storyboards "$storyboard_body")"
STORYBOARD_ID="$(field id <<<"$storyboard")"

frame="$(json_request POST "/storyboards/$STORYBOARD_ID/generate-frame" "$(jq -nc --argjson config "$IMAGE_CONFIG" '{frame_type:"first_frame",config_id:$config}')")"
jq -e '.data.image_url | length > 0' <<<"$frame" >/dev/null
video="$(json_request POST "/storyboards/$STORYBOARD_ID/generate-video" "$(jq -nc --argjson config "$VIDEO_CONFIG" '{config_id:$config}')")"
jq -e '.data.video_url | length > 0' <<<"$video" >/dev/null

wait_job() {
  local job_id="$1" deadline=$((SECONDS+120)) job state
  while (( SECONDS < deadline )); do
    job="$(json_request GET "/jobs/$job_id")"
    state="$(jq -r '.data.status' <<<"$job")"
    case "$state" in
      succeeded) return 0 ;;
      failed|canceled) jq . <<<"$job" >&2; return 1 ;;
    esac
    sleep 1
  done
  echo "job $job_id timed out; last state:" >&2
  jq . <<<"$job" >&2
  return 1
}

tts="$(json_request POST "/storyboards/$STORYBOARD_ID/generate-tts" '{}')"
wait_job "$(field job_id <<<"$tts")"
compose="$(json_request POST "/compose/episodes/$EPISODE_ID/compose-all" '{}')"
wait_job "$(field job_id <<<"$compose")"
merge="$(json_request POST "/merge/episodes/$EPISODE_ID/merge" '{}')"
wait_job "$(field job_id <<<"$merge")"

final="$(json_request GET "/dramas/$DRAMA_ID")"
jq -e --argjson episode_id "$EPISODE_ID" '.data.episodes[] | select(.id == $episode_id) | .video_url | length > 0' <<<"$final" >/dev/null
cache="$(json_request GET /organization/cache)"
jq -e '.data.objects > 0 and .data.references > 0 and .data.bytes > 0' <<<"$cache" >/dev/null
echo "live E2E passed: drama=$DRAMA_ID episode=$EPISODE_ID storyboard=$STORYBOARD_ID"
