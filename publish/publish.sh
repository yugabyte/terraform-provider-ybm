#!/bin/bash
echo 'Creating Version '$VERSION
sed -i 's/KEYID/'$KEY_ID'/g' version.json
sed -i 's/VERSION/'$VERSION'/g' version.json
version_response=$(curl -X POST --header "Content-Type: application/vnd.api+json"  --header "Authorization: Bearer $TOKEN" -d  @version.json https://app.terraform.io/api/v2/organizations/yugabyte/registry-providers/private/yugabyte/ybm/versions)
echo 'Version '$VERSION' Created'
echo 'Uploading SHASUMS files'
shasums_upload_url=$(echo $version_response | jq -r '.data.links."shasums-upload"')
shasums_sig_upload_url=$(echo $version_response | jq -r '.data.links."shasums-sig-upload"')
curl -T terraform-provider-ybm_${VERSION}_SHA256SUMS $shasums_upload_url
curl -T terraform-provider-ybm_${VERSION}_SHA256SUMS.sig $shasums_sig_upload_url
echo 'Uploaded SHASUMS files'
echo 'Uploading release files'
for os in darwin linux freebsd windows
do
    for arch in 386 amd64 arm arm64
    do
        if [[ $os == "darwin" && ($arch == "386" || $arch == "arm") ]]
        then
           continue
        fi
        echo 'Uploading release file for '$os'_'$arch
        shasum=$(sha256sum terraform-provider-ybm_${VERSION}_${os}_${arch}.zip | head -n1 | awk '{print $1;}')
        sed -i 's/VERSION/'$VERSION'/g' platform.json
        sed -i 's/OS/'$os'/g' platform.json
        sed -i 's/ARCH/'$arch'/g' platform.json
        sed -i 's/SHASUM/'$shasum'/g' platform.json
        platform_response=$(curl -X POST --header "Content-Type: application/vnd.api+json" --header "Authorization: Bearer $TOKEN" -d @platform.json https://app.terraform.io/api/v2/organizations/yugabyte/registry-providers/private/yugabyte/ybm/versions/$VERSION/platforms)
        binary_upload_url=$(echo $platform_response | jq -r '.data.links."provider-binary-upload"')
        curl -T terraform-provider-ybm_${VERSION}_${os}_${arch}.zip $binary_upload_url
        cp platform_backup.json platform.json
        echo 'Uploaded release file for '$os'_'$arch
    done
done
echo 'Uploaded release files'
