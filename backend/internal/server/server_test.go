package server

import "testing"

func TestBucketNameValidation(t *testing.T) {
 good:=[]string{"assets","user-files","bucket01","a12"}
 for _,v:=range good {if !bucketNamePattern.MatchString(v){t.Errorf("expected valid bucket name: %s",v)}}
 bad:=[]string{"ab","Uppercase","bad_name","-starts","ends-","two words"}
 for _,v:=range bad {if bucketNamePattern.MatchString(v){t.Errorf("expected invalid bucket name: %s",v)}}
}

func TestObjectKeyValidation(t *testing.T) {
 good:=[]string{"photo.png","folder/file.txt","folder/a b.txt"}
 for _,v:=range good {if !validObjectKey(v){t.Errorf("expected valid object key: %s",v)}}
 bad:=[]string{"","/absolute.txt","../escape.txt","folder/../escape","."}
 for _,v:=range bad {if validObjectKey(v){t.Errorf("expected invalid object key: %s",v)}}
}
