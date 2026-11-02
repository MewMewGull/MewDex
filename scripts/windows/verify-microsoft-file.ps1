param([Parameter(Mandatory=$true)][string]$Path)
$ErrorActionPreference = 'Stop'
$taskSignature = Get-AuthenticodeSignature -LiteralPath $Path
if ($taskSignature.Status -ne 'Valid' -or $taskSignature.SignerCertificate.Subject -notmatch 'O=Microsoft Corporation') {
    throw 'The WebView2 bootstrapper does not have a valid Microsoft signature.'
}
