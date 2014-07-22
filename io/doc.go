//This package is here to allow mocks to be generated of all the calls that touch the
//filesystem or docker.  With these, it is easy to write unit tests of the pickett logic.
//The interfaces in this package are designed to be independent and thus do not
//call each other.
package io
