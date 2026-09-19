$ErrorActionPreference = "Stop"

# Sign updater artifacts and write release metadata
if ([string]::IsNullOrWhiteSpace($env:UPDATE_PRIVATE_KEY)) {
  throw "OHNEGUESSR_UPDATE_PRIVATE_KEY is not configured"
}
$openssl = (Get-Command openssl -ErrorAction Stop).Source
$privateKey = Join-Path $env:RUNNER_TEMP update-private.pem
$publicDer = Join-Path $env:RUNNER_TEMP update-public.der
[IO.File]::WriteAllText($privateKey, $env:UPDATE_PRIVATE_KEY)
& $openssl pkey -in $privateKey -pubout -outform DER -out $publicDer
if ($LASTEXITCODE -ne 0) { throw "Invalid updater private key" }
$publicBytes = [IO.File]::ReadAllBytes($publicDer)
$publicRaw = $publicBytes[($publicBytes.Length - 32)..($publicBytes.Length - 1)]
if ([Convert]::ToBase64String($publicRaw) -ne $env:UPDATE_PUBLIC_KEY) {
  throw "Updater private key does not match the embedded public key"
}

function Sign-Artifact([string]$path) {
  $hash = (Get-FileHash $path -Algorithm SHA256).Hash.ToLowerInvariant()
  $digest = [Convert]::FromHexString($hash)
  $digestPath = Join-Path $env:RUNNER_TEMP "$([IO.Path]::GetFileName($path)).digest"
  $signaturePath = "$digestPath.sig"
  [IO.File]::WriteAllBytes($digestPath, $digest)
  & $openssl pkeyutl -sign -rawin -inkey $privateKey -in $digestPath -out $signaturePath
  if ($LASTEXITCODE -ne 0) { throw "Could not sign $path" }
  $result = [ordered]@{
    hash = $hash
    digest = [Convert]::ToBase64String($digest)
    signature = [Convert]::ToBase64String([IO.File]::ReadAllBytes($signaturePath))
    size = (Get-Item $path).Length
  }
  Remove-Item $digestPath, $signaturePath -Force
  return $result
}

$setupName = $env:SETUP_NAME
$portableName = $env:PORTABLE_NAME
$macUpdateName = $env:MAC_UPDATE_NAME
$debName = $env:DEB_NAME
$setup = Sign-Artifact "release/$setupName"
$portable = Sign-Artifact "release/$portableName"
$macUpdate = Sign-Artifact "release/$macUpdateName"
$deb = Sign-Artifact "release/$debName"
$base = "https://github.com/ohne-b/OhneGuessr/releases/download/$env:RELEASE_TAG"
$manifest = [ordered]@{
  schemaVersion = 1
  version = $env:RELEASE_VERSION
  notes = "[Check release notes on GitHub](https://github.com/ohne-b/OhneGuessr/releases/tag/$env:RELEASE_TAG)"
  # Keep these fields until pre-Wails clients have crossed this release.
  setup = [ordered]@{
    url = "$base/$setupName"
    sha256 = $setup.hash
    signature = $setup.signature
  }
  portable = [ordered]@{
    url = "$base/$portableName"
    sha256 = $portable.hash
    signature = $portable.signature
  }
  artifacts = @(
    [ordered]@{
      url = "$base/$portableName"
      filename = $portableName
      size = $portable.size
      platform = "windows"
      arch = "amd64"
      digestAlgo = "sha256"
      digest = $portable.digest
      signatureAlgo = "ed25519"
      signature = $portable.signature
    }
    [ordered]@{
      url = "$base/$debName"
      filename = $debName
      size = $deb.size
      platform = "linux"
      arch = "amd64"
      digestAlgo = "sha256"
      digest = $deb.digest
      signatureAlgo = "ed25519"
      signature = $deb.signature
    }
    [ordered]@{
      url = "$base/$macUpdateName"
      filename = $macUpdateName
      size = $macUpdate.size
      platform = "darwin"
      digestAlgo = "sha256"
      digest = $macUpdate.digest
      signatureAlgo = "ed25519"
      signature = $macUpdate.signature
    }
  )
}
$manifest | ConvertTo-Json -Depth 5 | Set-Content release/latest.json -Encoding utf8
Get-ChildItem release -File |
  Sort-Object Name |
  ForEach-Object {
    $hash = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $($_.Name)"
  } |
  Set-Content release/SHA256SUMS.txt -Encoding ascii
Remove-Item $privateKey, $publicDer -Force
