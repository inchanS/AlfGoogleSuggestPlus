# AlfGoogleSuggestPlus

[![테스트](https://github.com/inchanS/AlfGoogleSuggestPlus/actions/workflows/test-go.yml/badge.svg?branch=main)](https://github.com/inchanS/AlfGoogleSuggestPlus/actions/workflows/test-go.yml)
[![릴리스 빌드](https://github.com/inchanS/AlfGoogleSuggestPlus/actions/workflows/release.yml/badge.svg)](https://github.com/inchanS/AlfGoogleSuggestPlus/actions/workflows/release.yml)
[![GitHub 스타](https://img.shields.io/github/stars/inchanS/AlfGoogleSuggestPlus?style=flat&logo=apachespark)](https://github.com/inchanS/AlfGoogleSuggestPlus/stargazers)
[![누적 다운로드](https://img.shields.io/github/downloads/inchanS/AlfGoogleSuggestPlus/total?logo=github)](https://github.com/inchanS/AlfGoogleSuggestPlus/releases)
[![최신 릴리스](https://img.shields.io/github/v/release/inchanS/AlfGoogleSuggestPlus?logo=rocket)](https://github.com/inchanS/AlfGoogleSuggestPlus/releases/latest)
[![라이선스 MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

AlfNaverSearchPlus를 쓰면서 익숙해진 빠른 자동완성을 Google 검색에서도 쓰고 싶어 만들었습니다. Alfred 공식 Google Suggest는 입력이 바뀌면 이전 추천을 먼저 보여 주고, 0.1초 뒤 스크립트를 다시 실행해 새 추천을 가져옵니다. 이 워크플로우에서는 Alfred가 입력 원문을 즉시 검색할 수 있게 하고, Go 바이너리가 새 추천을 한 번의 실행으로 가져옵니다. 같은 검색어를 30초 안에 다시 입력하면 디스크 캐시에서 추천을 읽습니다.

같은 Mac에서 `서울 날씨`의 추천을 받아오는 데 걸린 중앙값은 공식 워크플로우 480.5 ms, 이 워크플로우 256.5 ms였고, 캐시 적중 시에는 7.8 ms였습니다. 측정 범위는 프로세스 시작부터 추천 JSON 반환까지입니다. [측정 조건과 결과](#측정-결과)

Alfred에서 `g 서울 날씨`처럼 입력하면 직접 검색 행과 Google 추천 검색어가 표시됩니다. ↩로 선택한 검색어를 기본 브라우저에서 열고, ⇥로 추천 검색어를 입력창에 채울 수 있습니다.

## 설치

Alfred Powerpack이 설치된 Mac에서 [AlfGoogleSuggestPlus.alfredworkflow](AlfGoogleSuggestPlus.alfredworkflow)를 열어 가져옵니다. 패키지에는 Apple Silicon과 Intel용 실행 코드가 함께 들어 있습니다. 사용 시 Go 설치는 필요하지 않습니다. **Configure Workflow → Search Keyword**에서 기본 키워드 `g`를 바꿀 수 있습니다. 같은 키워드를 쓰는 기존 워크플로우가 있으면 서로 다른 키워드를 지정하세요.

## 실행과 캐시

같은 키워드를 쓰는 Alfred 기본 Keyword와 Script Filter가 Open URL 동작 하나에 연결됩니다. Keyword가 입력 원문을 즉시 검색할 수 있게 하고, Script Filter가 `workflow/googlesuggest`를 외부 실행 파일로 직접 호출합니다. 이 구성을 택한 이유는 실제 Alfred에서 네트워크 요청 중 Enter를 누르면 직전 검색어가 열리는 현상을 재현했기 때문입니다. 직접 검색 행은 네트워크가 끊기거나 추천이 없어도 사용할 수 있습니다.

Go 바이너리는 검색어를 argv로 받고 JSON을 stdout에 한 번 출력한 뒤 종료합니다. 캐시가 비면 HTTPS 요청을 한 번 보냅니다. 요청 제한 시간은 1.5초입니다. 오류, 제한 초과, 비정상 응답에서는 추천 행을 비웁니다. 정상 응답은 Google 순서대로 출력하며 빈 항목과 직접 검색 행에 이미 표시한 원문은 생략합니다. 각 행은 원문 문자열을 Open URL에 넘기고 Alfred가 URL 쿼리 인코딩을 적용합니다.

검색어의 UTF-8 바이트를 MD5로 해시해 Alfred의 `alfred_workflow_cache` 아래 JSON 파일 이름으로 사용합니다. 캐시의 신선도는 수정 시각 기준 30초입니다. 캐시 적중은 네트워크를 사용하지 않으며, 정상적인 빈 추천 목록도 캐시합니다. 임시 파일 쓰기와 rename으로 쓰기를 마칩니다. 캐시를 쓸 때 약 하루에 한 번 24시간이 지난 파일을 정리합니다. 캐시 파일에는 검색어와 추천 목록이 포함됩니다. 동시에 같은 검색어의 캐시를 처음 조회하는 별개 프로세스는 각각 요청할 수 있습니다.

## 참고 소스와 차이

| 확인한 소스 | 구현에서 확인한 내용 |
| --- | --- |
| [AlfNaverSearchPlus · 6836e5f](https://github.com/inchanS/AlfNaverSearchPlus/tree/6836e5f1c4d49a0c3fd0b8b4e62f7bd2812be038) | 일반검색 `n`은 Script Filter → Bash 실행기 → Go의 search 핸들러 순서로 실행됩니다. Naver JSON의 `items[0]`를 순회해 Alfred JSON을 만들고, 검색 원문을 첫 행에 둡니다. 검색 캐시 30초, 요청 기한 3초, 업데이트 작업자의 캐시 정리 기준 24시간입니다. |
| [공식 Google Suggest · dc32c30](https://github.com/alfredapp/google-suggest-workflow/blob/dc32c304345df9493b1859123ab81ae288de6d91/Workflow/info.plist) | 새 입력은 JXA가 원문과 이전 추천을 먼저 반환합니다. 0.1초 후 재실행해 Google에 동기 요청하고 JSON 배열의 두 번째 원소를 읽습니다. 선택한 문자열은 Alfred의 Google 시스템 웹 검색에 전달합니다. |

Naver의 `cmd/`, `internal/cache/`, `workflow/` 구조와 Universal 빌드 → ad-hoc 서명 → ZIP 패키징 방식을 따릅니다. Go 표준 라이브러리만 사용합니다. 이번 구현에는 상주 프로세스나 자동 업데이트 작업자가 없으며, Alfred 재실행을 요청하는 `rerun`도 없습니다. Google용 Script Filter는 `queuedelaymode=0`, `queuemode=2`, `queuedelayimmediatelyinitially=true`를 사용합니다. Naver 일반검색의 해당 값은 각각 `0`, `1`, `false`입니다.

## Google 엔드포인트

공식 Alfred 예제와 같은 `https://suggestqueries.google.com/complete/search`에 `output=chrome`, `ie=utf8`, `oe=utf8`, `q=<검색어>`를 전달합니다. 캐시 미스 시 입력 중인 부분 검색어도 Google로 전송될 수 있습니다. Google은 이 자동완성 인터페이스를 비공개·미지원 API로 설명합니다. 응답 형식, 접근 가능 여부, 호출 한도가 바뀌면 추천 기능이 중단될 수 있습니다. [Google Search Central의 안내](https://developers.google.com/search/blog/2015/07/update-on-autocomplete-api)

## 빌드와 테스트

macOS에서 Go와 Xcode Command Line Tools를 사용할 수 있는 환경이 필요합니다.

```sh
go test -race ./...
go vet ./...
./make.sh
```

`./build.sh`는 arm64와 amd64를 각각 빌드해 `workflow/googlesuggest`로 결합하고 두 아키텍처의 서명을 확인합니다. `./make.sh`는 빌드부터 plist 검사와 압축 검증까지 실행한 뒤 루트에 설치 패키지를 생성합니다. 로컬 빌드는 `VERSION`에서, 태그 릴리스는 태그에서 버전을 읽어 배포용 plist의 `{{VERSION_INFO}}` 자리를 채웁니다. 배포 패키지에는 프로젝트의 [MIT 라이선스](LICENSE), AlfNaverSearchPlus에서 가져온 코드의 [원본 MIT 고지](workflow/LICENSE-Naver), 공식 Google Suggest 아이콘의 [BSD 3-Clause 고지](workflow/LICENSE-icon)가 포함됩니다.

### GitHub 릴리스

릴리스할 커밋에 `v1.0.0` 형식의 태그를 붙여 GitHub에 푸시합니다. 예를 들어 `main`과 해당 태그를 함께 올리는 명령은 `git push origin main v1.0.0`입니다. [릴리스 액션](.github/workflows/release.yml)은 태그의 숫자 부분을 배포용 plist 버전에 반영하고, macOS에서 Go 검사와 테스트를 통과한 Universal 패키지를 GitHub Release에 첨부합니다.

### 측정 결과

2026-11-27, macOS 27.2.1, Apple Silicon, Go 1.27.4. 같은 Mac에서 `서울 날씨`를 사용해 프로세스 시작부터 추천 JSON 반환까지 측정했습니다. Alfred 창의 렌더링과 키 입력 대기는 측정 범위 밖입니다.

| 실행 경로 | 횟수 | 중앙값 | 최소–최대 |
| --- | ---: | ---: | ---: |
| AlfGoogleSuggestPlus · 캐시 미스 | 7 | 256.5 ms | 248.5–263.6 ms |
| 공식 Google Suggest · 새 입력 | 7 | 480.5 ms | 471.5–502.8 ms |
| AlfGoogleSuggestPlus · 캐시 적중 | 35 | 7.8 ms | 6.8–9.2 ms |
| AlfNaverSearchPlus · 일반검색 캐시 적중 | 35 | 10.8 ms | 9.8–11.3 ms |

Google 비교는 두 구현을 번갈아 실행했습니다. 새 구현은 매회 해당 캐시를 지웠고, 공식 구현은 실제 plist의 JXA를 두 번 실행해 두 프로세스 시간과 지정된 재실행 간격 100 ms를 합산했습니다. 캐시 적중 비교는 예열 후 Google의 실제 응답과 Naver 형식의 추천 15개 고정 데이터를 사용했습니다. Naver는 같은 커밋의 로컬 바이너리를 정상 실행기 경유로 호출했습니다. 네트워크 시간은 서버와 연결 상태에 따라 달라집니다.

실제 Google에서 한국어·영어·혼합·특수문자·분해형 한글의 추천을 확인했습니다. 자동 테스트는 순서, 입력 인코딩, 빈 결과, 캐시 적중·만료·손상, 연결 실패, 타임아웃, HTTP 429, 잘못된 응답 처리를 검증합니다. Alfred 6와 Safari에서는 연속 입력 직후 Enter로 전체 검색어가 열린 경우와 추천 행을 골라 검색한 경우를 확인했습니다. x86_64 슬라이스와 서명은 검사했으며 Intel 하드웨어에서의 실행은 검증하지 못했습니다.
