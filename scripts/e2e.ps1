# End-to-end проверка апплаенса: поднимает контейнер на локальном PBF или по URL,
# дожидается импорта, прогоняет эталонные кейсы и проверяет, что данные переживают
# пересоздание контейнера (БД и тайлы лежат в volume /data).
#
#   powershell -File scripts/e2e.ps1 -PbfPath D:\osm\belarus-latest.osm.pbf
#   powershell -File scripts/e2e.ps1 -PbfUrl  https://download.geofabrik.de/europe/belarus-latest.osm.pbf
param(
    [string]$PbfPath = "",
    [string]$PbfUrl = "",
    [string]$Image = "aliakseikarpenka/slup-geo:dev",
    [string]$Container = "slup-geo-e2e",
    [int]$Port = 8083,
    [int]$WaitMinutes = 50,
    [switch]$KeepData
)

$ErrorActionPreference = "Continue"
$failures = 0
$volume = "$Container-data"
$base = "http://localhost:$Port"

function Show([string]$name, [bool]$ok, [string]$detail) {
    if ($ok) { Write-Output "OK:   $name" }
    else { Write-Output "FAIL: $name — $detail"; $script:failures++ }
}

function Wait-Healthy([int]$minutes) {
    $deadline = (Get-Date).AddMinutes($minutes)
    while ((Get-Date) -lt $deadline) {
        if ((docker inspect -f '{{.State.Running}}' $Container 2>$null) -ne "true") { return $false }
        try {
            $health = Invoke-RestMethod "$base/health" -TimeoutSec 3
            if ($health.status -eq "ok") { return $true }
        } catch { }
        Start-Sleep -Seconds 10
    }
    return $false
}

docker rm -f $Container 2>$null | Out-Null
if (-not $KeepData) {
    docker volume rm $volume 2>$null | Out-Null
}
docker volume create $volume | Out-Null

$runArgs = @("-d", "--name", $Container, "-p", "${Port}:8080", "-v", "${volume}:/data")
if ($PbfUrl) {
    Write-Output "PBF URL: $PbfUrl"
    $runArgs += @("-e", "PBF_URL=$PbfUrl")
} elseif ($PbfPath) {
    $pbf = (Resolve-Path $PbfPath).Path
    Write-Output "PBF: $pbf"
    $runArgs += @("-v", "${pbf}:/data/osm/belarus-latest.osm.pbf:ro")
} else {
    Write-Output "Нужен -PbfPath (локальный дамп) или -PbfUrl (скачать)"
    exit 2
}

docker run @runArgs $Image | Out-Null

if (-not (Wait-Healthy $WaitMinutes)) {
    Write-Output "--- логи контейнера ---"
    docker logs --tail 150 $Container
    Write-Output "E2E: сервис не поднялся"
    exit 1
}

Show "health" $true ""

$p1 = Invoke-RestMethod "$base/v1/point?lat=52.3955063&lon=30.9607992"
Show "point: дом с адресом" ($p1.text -like "*Григория Денисенко, д. 22*") $p1.text
Show "point: дом первым" ($p1.zones[0].level -eq "HOUSE") ($p1.zones[0] | ConvertTo-Json -Compress)

$p2 = Invoke-RestMethod "$base/v1/point?lat=52.437861&lon=30.9972"
Show "point: Полесская, д. 14" ($p2.text -like "*Полесская*14*") $p2.text

$p3 = Invoke-RestMethod "$base/v1/point?lat=52.3021783&lon=30.8780772"
$p3json = $p3.zones | ConvertTo-Json -Compress
Show "point: Бобовичский сельсовет" ($p3json -like "*Бобовичский*") $p3json

$q = [uri]::EscapeDataString("Бородина")
$s1 = Invoke-RestMethod "$base/v1/suggest?q=$q"
Show "suggest: дома улицы Бородина" ($s1.items.Count -gt 0 -and $s1.items[0].level -eq "HOUSE" -and $s1.items[0].name -like "*Бородина*") ($s1.items[0] | ConvertTo-Json -Compress)

$q = [uri]::EscapeDataString("Гомель")
$s2 = Invoke-RestMethod "$base/v1/suggest?q=$q"
Show "suggest: Гомель — город первым" ($s2.items[0].level -eq "CITY") ($s2.items[0] | ConvertTo-Json -Compress)

$q = [uri]::EscapeDataString("3я линейная")
$s3 = Invoke-RestMethod "$base/v1/suggest?q=$q"
Show "suggest: 3-я Линейная" ($s3.items[0].level -eq "HOUSE" -and $s3.items[0].name -like "*3-я Линейная*") ($s3.items[0] | ConvertTo-Json -Compress)

$q = [uri]::EscapeDataString("Железнодорожный")
$s4 = Invoke-RestMethod "$base/v1/suggest?q=$q"
Show "suggest: район первым" ($s4.items[0].level -in @("CITY_DISTRICT", "RAION")) ($s4.items[0] | ConvertTo-Json -Compress)

$q = [uri]::EscapeDataString("14 Полесская")
$s5 = Invoke-RestMethod "$base/v1/suggest?q=$q"
Show "suggest: 14 Полесская = дом 14" ($s5.items[0].level -eq "HOUSE" -and $s5.items[0].name -like "*д. 14*") ($s5.items[0] | ConvertTo-Json -Compress)

$q = [uri]::EscapeDataString("Полесская 14")
$s6 = Invoke-RestMethod "$base/v1/suggest?q=$q"
Show "suggest: Полесская 14 = дом 14" ($s6.items[0].level -eq "HOUSE" -and $s6.items[0].name -like "*д. 14*") ($s6.items[0] | ConvertTo-Json -Compress)

$areaId = $s4.items[0].id
$a = Invoke-RestMethod "$base/v1/areas/$areaId"
Show "areas: MultiPolygon" ($a.geometry.type -in @("Polygon", "MultiPolygon")) $a.geometry.type

$tiles = Invoke-RestMethod "$base/tiles/tiles.json"
Show "tiles: манифест" ($null -ne $tiles.files) ($tiles | ConvertTo-Json -Compress)
if ($tiles.files.Count -gt 0) {
    $tileName = $tiles.files[0].name
    $magic = (& curl.exe -s -r 0-6 "$base/tiles/$tileName")
    Show "tiles: архив PMTiles (Range)" ($magic -eq "PMTiles") "magic=$magic"
} else {
    Show "tiles: архив PMTiles (Range)" $false "манифест пуст"
}

$style = Invoke-RestMethod "$base/tiles/style.json"
Show "tiles: стиль с относительным источником" ($style.sources.openmaptiles.url -eq "pmtiles:///tiles/basemap.pmtiles") ($style.sources.openmaptiles.url)

$aliasMagic = (& curl.exe -s -r 0-6 "$base/tiles/basemap.pmtiles")
Show "tiles: алиас basemap.pmtiles" ($aliasMagic -eq "PMTiles") "magic=$aliasMagic"

$fontCode = (& curl.exe -s -o NUL -w "%{http_code}" "$base/tiles/fonts/Noto%20Sans%20Regular/0-255.pbf")
Show "tiles: глифы" ($fontCode -eq "200") "http=$fontCode"

$exampleCode = (& curl.exe -s -L -o NUL -w "%{http_code}" "$base/example")
Show "example: страница" ($exampleCode -eq "200") "http=$exampleCode"

$h = Invoke-RestMethod "$base/health"
Show "health: importedAt и lastUpdate" ($null -ne $h.importedAt -and $null -ne $h.lastUpdate) ($h | ConvertTo-Json -Compress)

$status = Invoke-RestMethod "$base/status"
Show "status: schemaReady и данные" ($status.schemaReady -eq $true -and $null -ne $status.data.importedAt) ($status | ConvertTo-Json -Depth 4 -Compress)

$longQ = "a" * 200
try {
    Invoke-RestMethod "$base/v1/suggest?q=$longQ" -ErrorAction Stop | Out-Null
    Show "suggest: лимит длины q" $false "вернулось 200 вместо 400"
} catch {
    Show "suggest: лимит длины q" ($_.Exception.Response.StatusCode.value__ -eq 400) "http=$($_.Exception.Response.StatusCode.value__)"
}

try {
    Invoke-RestMethod "$base/v1/areas/W-1?simplify=weird" -ErrorAction Stop | Out-Null
    Show "areas: simplify enum" $false "вернулось 200 вместо 400"
} catch {
    Show "areas: simplify enum" ($_.Exception.Response.StatusCode.value__ -eq 400) "http=$($_.Exception.Response.StatusCode.value__)"
}

$vendorCode = (& curl.exe -s -o NUL -w "%{http_code}" "$base/tiles/vendor/maplibre-gl.mjs")
$vendorType = (& curl.exe -s -o NUL -w "%{content_type}" "$base/tiles/vendor/maplibre-gl.mjs")
Show "tiles: вендорные модули" ($vendorCode -eq "200" -and $vendorType -like "text/javascript*") "http=$vendorCode type=$vendorType"

# Данные должны переживать пересоздание контейнера: БД и тайлы лежат в /data (volume).
docker rm -f $Container | Out-Null
docker run -d --name $Container -p "${Port}:8080" -v "${volume}:/data" $Image | Out-Null
if (Wait-Healthy 5) {
    $p = Invoke-RestMethod "$base/v1/point?lat=52.3955063&lon=30.9607992"
    Show "данные переживают пересоздание контейнера" ($p.text -like "*Григория Денисенко*") $p.text
} else {
    Show "данные переживают пересоздание контейнера" $false "сервис не поднялся после пересоздания"
}

Write-Output ""
if ($failures -eq 0) { Write-Output "E2E: все проверки пройдены" } else { Write-Output "E2E: ошибок — $failures" }
exit $failures
