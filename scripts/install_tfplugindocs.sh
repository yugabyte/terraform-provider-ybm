#!/bin/bash                                                                                          
if [ -z "$(which tfplugindocs)" ]; then                                                                 
  echo "tfplugindocs not found. Downloading now..."                                                  
  tag=$(curl --silent "https://api.github.com/repos/hashicorp/terraform-plugin-docs/releases/latest" | jq -r .tag_name)
  version="${tag:1}"                                                                                    
  download_url=$(curl -s https://api.github.com/repos/hashicorp/terraform-plugin-docs/releases/latest | grep -E 'browser_download_url' | grep $1 | cut -d '"' -f 4)
  wget ${download_url}                                                                                  
  mkdir tmp                                                                                             
  unzip -o tfplugindocs_${version}_${1}.zip -d tmp/                                                      
  mv tmp/tfplugindocs /usr/local/bin/                                                                   
  rm -rf tmp                                                                                            
  rm -rf tfplugindocs_${version}_${1}.zip                                                               
fi 
